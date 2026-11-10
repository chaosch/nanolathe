package main

import (
	"errors"
	"fmt"
	"github.com/hajimehoshi/ebiten/v2"
	"github.com/nanolathe-gg/nanolathe/internal/client"
	"github.com/nanolathe-gg/nanolathe/internal/film"
	"github.com/nanolathe-gg/nanolathe/internal/i18n"
	"github.com/nanolathe-gg/nanolathe/internal/platform/screenkit"
	"os"
	"path/filepath"
	"strings"
)

// The interface language (docs/DESIGN_I18N.md): the saved choice, a
// --language flag over it for one run, and the Language card that changes it.
// Text is translated where the Nanolathe screen sets it (internal/i18n).

// parseLanguage is the --language flag's value as a language code.
func parseLanguage(text string) (string, error) {
	code := strings.ToLower(strings.TrimSpace(text))
	if !i18n.Known(code) {
		codes := make([]string, len(i18n.Languages))
		for i, l := range i18n.Languages {
			codes[i] = l.Code
		}
		return "", fmt.Errorf("nanolathe: unknown language %q: logical path <command line>, providers searched [language], expected one of %s", text, strings.Join(codes, ", "))
	}
	return code, nil
}

// applyLanguage selects the interface language: the --language flag for this
// run, otherwise the saved choice. A catalog that would not load leaves the
// interface in English with a notice.
func (g *gameShell) applyLanguage() {
	code := g.requestedLanguage()

	// 1. 如果當前已經生效的語言和目標一致，且沒有待處理的切換，直接返回，避免死循環。
	if code == i18n.Language() && g.languagePending == "" {
		return
	}

	// 2. 找出目標語言是否需要 CJK
	var isCJK bool
	for _, l := range i18n.Languages {
		if l.Code == code {
			isCJK = l.IsCJK
			break
		}
	}

	// 3. 清理所有舊緩存
	client.ClearCjkTranslateCache()
	renderedButtonCache.Clear()
	// 如果 film 有 ResetFontCache 也加上：film.ResetFontCache()

	// 4. 如果是 CJK，啟動異步加載
	if isCJK {
		g.languagePending = code
		film.EnableCJKForLanguage(code)
		return
	}

	// 5. 非 CJK（英文、烏克蘭），同步直接切換字典
	g.languagePending = ""
	g.setInterfaceLanguage(code)
}

func (g *gameShell) requestedLanguage() string {
	code := g.language
	if g.opts.Language != "" {
		code = g.opts.Language
	}
	if code == "" {
		code = i18n.English
	}
	return code
}

func (g *gameShell) setInterfaceLanguage(code string) {
	if err := i18n.SetLanguage(code); err != nil {
		fmt.Fprintf(os.Stderr, "nanolathe: %v (using English)\n", err)
	}
}

// A browser download completes between host steps. Activate the catalog only
// once its glyphs exist; a failed request leaves the prior language readable
// and the card's unapplied choice available for another Apply (DESIGN_I18N).
func (g *gameShell) pollLanguage() {
	if g.languagePending != "" {
		g.completeLanguage(film.CJKReady(), film.CJKError())
	}
}

func (g *gameShell) completeLanguage(ready bool, err error) {
	if g.languagePending == "" {
		return
	}
	if err != nil {
		code := g.languagePending
		g.languagePending = ""
		fmt.Fprintf(os.Stderr, "nanolathe: %s fonts failed: logical path film, providers searched [browser], expected CJK atlases: %v\n", code, err)
		if s := nlScreenInst; s != nil && s.open && s.shell() == g {
			s.retainLanguageChoice(g)
			s.toast, s.toastLeft = fmt.Sprintf("%s fonts could not load. Apply to retry.", code), 6
		}
		return
	}
	if ready {
		code := g.languagePending
		g.languagePending = ""
		g.setInterfaceLanguage(code)
		if s := nlScreenInst; s != nil && s.open && s.shell() == g {
			if !s.touched["language"] {
				s.draft.language = languageIndex()
			}
			s.toast, s.toastLeft = "Settings saved", 2.2
		}
	}
}

// chooseLanguage makes the Language card's choice the saved language. It
// wins over a --language flag from then on.
func (g *gameShell) chooseLanguage(i int) {
	code := i18n.Languages[i].Code
	if code == i18n.Language() && code == g.requestedLanguage() && g.languagePending == "" {
		return
	}
	g.opts.Language = ""
	g.language = code
	if code == i18n.English {
		g.language = ""
	}
	g.applyLanguage()
}

// languageIndex is the running language's place in i18n.Languages.
func languageIndex() int {
	return languageCodeIndex(i18n.Language())
}

func languageCodeIndex(code string) int {
	for i, l := range i18n.Languages {
		if l.Code == code {
			return i
		}
	}
	return 0
}

// A shell rebind can occur before an optional download completes. Keep its
// requested choice available for Apply without replacing a newer draft edit.
func (s *nlScreen) retainLanguageChoice(g *gameShell) {
	req := g.requestedLanguage()
	// 如果目標是需要 CJK 的語言，且當前還沒切換過去
	if i18n.LanguageNeedCJK(req) && i18n.Language() != req && !s.touched["language"] {
		s.draft.language = languageCodeIndex(req)
	}
}

// languageCard is the Game page's Language card. Each language is named, and
// described under its name, in its own words, so a player who cannot read the
// running language still finds theirs.
func (s *nlScreen) languageCard() nlCard {
	steps := make([]string, len(i18n.Languages))
	subs := make([]string, len(i18n.Languages))

	for i, l := range i18n.Languages {
		// 預設顯示它自己的名字
		steps[i] = l.Name
		subs[i] = l.Name + " interface"

		// 特殊處理英文和烏克蘭的專屬小字
		if l.Code == i18n.English {
			subs[i] = "The interface as written"
		} else if l.Code == "ukrainian" {
			subs[i] = "Інтерфейс українською"
		}

		// 【核心邏輯】：如果這個語言是 CJK，且字體還沒加載好（防豆腐塊）
		if l.IsCJK && !film.CJKReady() {
			steps[i] = l.EnglishName
			subs[i] = l.EnglishName + " interface"
		}
	}

	return nlCard{
		key: "language", label: "Language", pics: []string{"armcom", "corcom"}, kind: nlHalves,
		steps: steps, subs: subs,
		get: func(d *nlDraft) int { return d.language },
		set: func(d *nlDraft, v int) { d.language = v },
		desc: func(*nlDraft, int) string {
			return "The language of this screen. The menus drawn in the original game's art, the battle interface and the units' own names stay in English for now."
		},
		scene: func(*nlDraft, int) string { return "armor" },
	}
}

// fitStyle is st set smaller, down to smallest, until text fits width: a
// translation can be longer than the English a place was laid out for.
func fitStyle(f *screenkit.Font, text string, st screenkit.Style, width, smallest float64) screenkit.Style {
	if w := f.Measure(text, st); w > width && w > 0 {
		st.Size = max(smallest, st.Size*width/w)
	}
	return st
}

// cardCaption sets a carousel card's value line, smaller when it would run
// past right.
func (s *nlScreen) cardCaption(screen *ebiten.Image, text string, x, y, right, a float64) {
	u := s.u()
	st := screenkit.Style{Size: 10.5 * u, Top: alphaC(nlGreenText, a)}
	s.fonts.Body.Draw(screen, text, x, y, fitStyle(s.fonts.Body, text, st, right-x, 8*u))
}

func defaultLocalesDir() (string, bool) {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			fmt.Fprintln(os.Stderr, "nanolathe: data folder not found")
			return "", false
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "nanolathe", "locales"), true
}

func createLocalesDir() (string, bool) {
	if localesDir, r := defaultLocalesDir(); r {
		if exist, _ := DirExists(localesDir); exist {
			return localesDir, true
		}
		if err := os.MkdirAll(localesDir, 0755); err != nil {
			fmt.Fprintln(os.Stderr, "nanolathe: cannot create locales directory:"+err.Error())
			return localesDir, false
		} else {
			return localesDir, true
		}
	}
	return "", false
}

// DirExists 判断目录是否存在
func DirExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		// 如果错误是“文件或目录不存在”，返回 false, nil
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		// 其他错误（比如权限不足），返回 false 和具体错误
		return false, err
	}

	// 路径存在，但检查它是不是目录
	if !info.IsDir() {
		return false, fmt.Errorf("路径 %s 存在，但不是一个目录", path)
	}

	return true, nil
}

// If missing, fallback to embedded localization assets.

func verifyLocalesFolder(baseDir, folder string) bool {
	files := []string{"tiny-cjk.png", "normal-cjk.png", "small-cjk.json", "normal-cjk.json", "body-cjk.png", "tiny-cjk.json", "display-cjk.png", "small-cjk.png", "display.json", "display-cjk.json", "body-cjk.json", "display.png", "body.png", "body.json"}
	files = append(files, folder+".json")
	for _, file := range files {
		filename := filepath.Join(baseDir, folder, file)
		if info, err := os.Stat(filename); err != nil || info.IsDir() {
			return false
		}
	}
	return true
}
