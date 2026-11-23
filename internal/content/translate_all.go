package content

import (
	"fmt"
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/i18n"
	"github.com/nanolathe-gg/nanolathe/vfs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//func ApplyTranslationTable(translationTable *TranslationTable) error {
//	ntt := NewTranslationTable(make(map[string]string))
//	for k, v := range translationTable {
//		translationTable[k] = v
//	}
//}

type UnitTranslation struct {
	Name            string
	Description     string
	NameFrom        string
	DescriptionFrom string
}

type CampaignTranslation struct {
	Code string
	Name string
}

type MissionTranslation struct {
	Code         string
	Name         string
	File         string
	CampaignCode string
	Briefing     []byte
}

type BriefTranslation struct {
	Name string
	Text []byte
}

type UITranslation struct {
	Code string
	Text string
}

type LanguagePack struct {
	Briefs    map[string]BriefTranslation
	Units     map[string]UnitTranslation
	Missions  map[string]MissionTranslation
	Campaigns map[string]CampaignTranslation
	UI        map[string]UITranslation
	Features  map[string]UITranslation // 新增：feature 翻译
	Full      bool
}

type TranslationPack struct {
	looseDir string
	modeName string

	transFS *vfs.FS
	gameFS  vfs.FSOps

	Packs map[string]*LanguagePack
	Order []string
}

var GlobalTranslations = &TranslationPack{
	Packs: make(map[string]*LanguagePack),
}

func newLanguagePack() *LanguagePack {
	return &LanguagePack{
		Briefs:    make(map[string]BriefTranslation),
		Units:     make(map[string]UnitTranslation),
		Missions:  make(map[string]MissionTranslation),
		Campaigns: make(map[string]CampaignTranslation),
		UI:        make(map[string]UITranslation),
		Features:  make(map[string]UITranslation),
	}
}

func (p *TranslationPack) pack(lang string) *LanguagePack {
	if p.Packs == nil {
		p.Packs = make(map[string]*LanguagePack)
	}
	lp, ok := p.Packs[lang]
	if !ok {
		lp = newLanguagePack()
		p.Packs[lang] = lp
	}
	return lp
}

// ============================================================================
// 查询方法 (包含 English 回退)
// ============================================================================

func (p *TranslationPack) GetUnit(lang, unitKey string) (UnitTranslation, bool) {
	if p == nil {
		return UnitTranslation{}, false
	}
	key := CanonicalKey(unitKey)

	// 1. 优先查指定语言
	lp, ok := p.Packs[strings.ToLower(strings.TrimSpace(lang))]
	if ok {
		if t, found := lp.Units[key]; found {
			return t, true
		}
	}

	// 2. 如果没找到，且不是 english，回退到 english
	if !strings.EqualFold(lang, "english") {
		if enLp, ok := p.Packs["english"]; ok {
			if t, found := enLp.Units[key]; found {
				return t, true
			}
		}
	}

	return UnitTranslation{}, false
}

func (p *TranslationPack) GetUI(lang, code string) (UITranslation, bool) {
	if p == nil {
		return UITranslation{}, false
	}
	code = strings.TrimSpace(code)

	// 1. 优先查指定语言
	lp, ok := p.Packs[strings.ToLower(strings.TrimSpace(lang))]
	if ok {
		if u, found := lp.UI[code]; found {
			return u, true
		}
	}

	// 2. 如果没找到，且不是 english，回退到 english
	if !strings.EqualFold(lang, "english") {
		if enLp, ok := p.Packs["english"]; ok {
			if u, found := enLp.UI[code]; found {
				return u, true
			}
		}
	}

	return UITranslation{}, false
}

// GetCampaign 无回退
func (p *TranslationPack) GetCampaign(lang, code string) (CampaignTranslation, bool) {
	if p == nil || code == "" {
		return CampaignTranslation{}, false
	}
	lp, ok := p.Packs[strings.ToLower(strings.TrimSpace(lang))]
	if !ok {
		return CampaignTranslation{}, false
	}
	key := strings.TrimSuffix(strings.TrimSpace(code), ".tdf")
	c, ok := lp.Campaigns[key]
	return c, ok
}

// GetMission 无回退
func (p *TranslationPack) GetMission(lang, otaFile string) (MissionTranslation, bool) {
	if p == nil || otaFile == "" {
		return MissionTranslation{}, false
	}
	lp, ok := p.Packs[strings.ToLower(strings.TrimSpace(lang))]
	if !ok {
		return MissionTranslation{}, false
	}
	key := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(otaFile)), ".ota")
	m, ok := lp.Missions[key]
	return m, ok
}

// GetBrief 无回退
func (p *TranslationPack) GetBrief(lang, briefName string) (BriefTranslation, bool) {
	if p == nil || briefName == "" {
		return BriefTranslation{}, false
	}
	lp, ok := p.Packs[strings.ToLower(strings.TrimSpace(lang))]
	if !ok {
		return BriefTranslation{}, false
	}
	key := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(briefName)), ".txt")
	b, ok := lp.Briefs[key]
	return b, ok
}

func (p *TranslationPack) Languages() []string {
	if p == nil {
		return nil
	}
	return p.Order
}

func (p *TranslationPack) GetPack(lang string) (*LanguagePack, bool) {
	if p == nil {
		return nil, false
	}
	lp, ok := p.Packs[strings.ToLower(strings.TrimSpace(lang))]
	return lp, ok
}

// ============================================================================
// 加载环境管理
// ============================================================================

func (p *TranslationPack) Clear() {
	p.Packs = make(map[string]*LanguagePack)
	p.Order = nil
}

func (p *TranslationPack) Init(looseDir string, sel string, gameFS vfs.FSOps) error {
	p.Clear()
	p.looseDir = looseDir
	p.modeName = sel
	p.gameFS = gameFS

	fmt.Printf("[Translation] 开始挂载翻译包根目录 (locales): %s\n", looseDir)

	tmp := vfs.New()
	if err := tmp.MountDirectory(looseDir, 5000); err != nil {
		tmp.Close()
		return fmt.Errorf("[Translation] 挂载失败: %w", err)
	}
	p.transFS = tmp
	return nil
}

func (p *TranslationPack) Close() {
	if p.transFS != nil {
		p.transFS.Close()
		p.transFS = nil
	}
	p.gameFS = nil
	fmt.Printf("[Translation] 翻译包已卸载\n")
}

// ============================================================================
// 统一加载入口
// ============================================================================

func (p *TranslationPack) LoadAll(looseDir string, sel string, gameFS vfs.FSOps) {
	if err := p.Init(looseDir, sel, gameFS); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return
	}
	defer p.Close()

	// 1. 先加载游戏原版自带的数据 (Native)
	fmt.Printf("[Translation] 提取游戏原版单位与 UI...\n")
	p.loadGameNativeUnits()
	p.loadGameNativeTranslations()

	// 2. 扫描 locales 下实际存在的本地语言目录
	entries, err := p.transFS.ReadDir(".")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[Translation] 读取语言目录失败: %v\n", err)
		return
	}

	localDirs := make(map[string]bool)
	for _, e := range entries {
		if !e.IsDir {
			continue
		}
		lang := strings.ToLower(e.Path)
		if lang != "" {
			localDirs[lang] = true
		}
	}

	// 3. 根据 Languages 定义和实际存在的目录，按序加载本地包
	var finalOrder []string
	for _, langDef := range i18n.Languages {
		langKey := strings.ToLower(langDef.Code)

		if localDirs[langKey] {
			fmt.Printf("[Translation] 开始加载本地语言包: %s (%s)\n", langKey, langDef.Name)
			p.loadAllForLang(langKey)
			finalOrder = append(finalOrder, langKey)
			delete(localDirs, langKey)
		} else if _, exists := p.Packs[langKey]; exists {
			finalOrder = append(finalOrder, langKey)
		}
	}

	// 4. 处理剩余未在 Languages 中定义但实际存在的语言
	var remainingLocal []string
	for lang := range localDirs {
		remainingLocal = append(remainingLocal, lang)
	}
	sort.Strings(remainingLocal)
	for _, lang := range remainingLocal {
		fmt.Printf("[Translation] 开始加载未知本地语言包: %s\n", lang)
		p.loadAllForLang(lang)
		finalOrder = append(finalOrder, lang)
	}

	var remainingPacks []string
	for lang := range p.Packs {
		found := false
		for _, l := range finalOrder {
			if l == lang {
				found = true
				break
			}
		}
		if !found {
			remainingPacks = append(remainingPacks, lang)
		}
	}
	sort.Strings(remainingPacks)
	finalOrder = append(finalOrder, remainingPacks...)

	p.Order = finalOrder

	// 5. 打印统计
	for _, lang := range p.Order {
		lp := p.Packs[lang]
		fmt.Printf("[Translation] [%s] 单位 %d, 任务 %d, 战役 %d, UI %d 项\n",
			lang, len(lp.Units), len(lp.Missions), len(lp.Campaigns), len(lp.UI))
	}
	// 6.打印所有features
	//e, _ := gameFS.ReadDir("features")
	//for _, fi := range e {
	//	file, _ := gameFS.ReadDir(filepath.Join("features", fi.Name))
	//	for _, f := range file {
	//		data, _ := gameFS.ReadFileLimit(filepath.Join("features", fi.Name, f.Name), 4<<20)
	//		doc, _ := formats.ParseTDF(data)
	//		for _, section := range doc.Root.Sections() {
	//			//world, _ := section.StringValue("world", "")
	//			desc, _ := section.StringValue("description", "")
	//			//cata, _ := section.StringValue("category", "")
	//			fmt.Println(desc)
	//		}
	//	}
	//}
}

// ============================================================================
// 内部加载逻辑
// ============================================================================

func (p *TranslationPack) loadAllForLang(lang string) {
	lp := p.pack(lang)

	// 1. 英文底座：gameFS 的 camps/briefs 全部 txt
	p.fillNativeBriefs(lang, lp)

	p.loadUnits(lang, "ota")
	p.loadCampaignsAndMissions(lang, "ota")
	p.loadUITranslations(lang, "ota")
	p.loadFeatureTranslations(lang, "ota") // 新增
	if p.modeName != "" && p.modeName != "ota" {
		p.loadUnits(lang, p.modeName)
		p.loadCampaignsAndMissions(lang, p.modeName)
		p.loadUITranslations(lang, p.modeName)
		p.loadFeatureTranslations(lang, p.modeName) // 新增
	}
	//lp := p.pack(lang)
	lp.Full = true
}

func (p *TranslationPack) loadUnits(lang, modName string) {
	path := filepath.Join(lang, "loose", "translation/mods", modName, "units.tdf")
	data, err := p.transFS.ReadFileLimit(path, 4<<20)
	if err != nil {
		return
	}
	doc, err := formats.ParseTDF(data)
	lp := p.pack(lang)
	count := 0
	for _, section := range doc.Root.Sections() {
		unitKey := CanonicalKey(section.OriginalName)
		if unitKey == "" {
			continue
		}

		name, _ := section.StringValue("name", "")
		description, _ := section.StringValue("description", "")
		nameFrom, _ := section.StringValue("name-from", "")
		descriptionFrom, _ := section.StringValue("description-from", "")

		lp.Units[unitKey] = UnitTranslation{
			Name:            strings.TrimSpace(name),
			Description:     strings.TrimSpace(description),
			NameFrom:        strings.TrimSpace(nameFrom),
			DescriptionFrom: strings.TrimSpace(descriptionFrom),
		}
		count++
	}
	if count > 0 {
		fmt.Printf("[Translation]   - [%s] 载入单位翻译 %d 项 (%s)\n", lang, count, path)
	}
}

func (p *TranslationPack) loadGameNativeUnits() {
	if p.gameFS == nil {
		return
	}

	entries, err := p.gameFS.ReadDir("units")
	if err != nil {
		return
	}

	counts := make(map[string]int)

	for _, e := range entries {
		if e.IsDir || !strings.HasSuffix(strings.ToLower(e.Path), ".fbi") {
			continue
		}

		data, err := p.gameFS.ReadFileLimit(e.Path, 2<<20)
		if err != nil {
			continue
		}

		doc, err := formats.ParseTDF(data)
		if err != nil || doc.Root == nil {
			continue
		}

		sec := doc.Root.Section("UNITINFO")
		if sec == nil {
			sec = doc.Root.Section("unitinfo")
		}
		if sec == nil && len(doc.Root.Sections()) > 0 {
			sec = doc.Root.Sections()[0]
		}
		if sec == nil {
			continue
		}

		unitName, _ := sec.StringValue("UnitName", "")
		unitKey := CanonicalKey(unitName)
		if unitKey == "" {
			unitKey = CanonicalKey(strings.TrimSuffix(filepath.Base(e.Path), ".fbi"))
		}
		if unitKey == "" {
			continue
		}

		// 1. 提取英文 (Name / Description)
		name, _ := sec.StringValue("Name", "")
		desc, _ := sec.StringValue("Description", "")
		if name != "" || desc != "" {
			enLp := p.pack("english")
			enLp.Units[unitKey] = UnitTranslation{
				Name:        strings.TrimSpace(name),
				Description: strings.TrimSpace(desc),
			}
			counts["english"]++
		}

		// 2. 提取其他多语言 (直接基于 Languages 处理)
		for _, langDef := range i18n.Languages {
			if langDef.Code == "english" {
				continue
			}
			fieldName := strings.Title(langDef.Code)
			name, _ := sec.StringValue(fieldName+"Name", "")
			desc, _ := sec.StringValue(fieldName+"Description", "")

			if name != "" || desc != "" {
				lp := p.pack(langDef.Code)
				lp.Units[unitKey] = UnitTranslation{
					Name:        strings.TrimSpace(name),
					Description: strings.TrimSpace(desc),
				}
				counts[langDef.Code]++
			}
		}
	}

	for lang, count := range counts {
		if count > 0 {
			fmt.Printf("[Translation]   - 从游戏本体提取到 [%s] 单位数据 %d 项\n", lang, count)
		}
	}
}

func (p *TranslationPack) loadGameNativeTranslations() {
	if p.gameFS == nil {
		return
	}

	path := "gamedata/translate.tdf"
	data, err := p.gameFS.ReadFileLimit(path, 4<<20)
	if err != nil {
		return
	}

	doc, err := formats.ParseTDF(data)
	if err != nil || doc.Root == nil {
		return
	}

	counts := make(map[string]int)

	for _, sec := range doc.Root.Sections() {
		code := strings.TrimSpace(sec.OriginalName)
		if code == "" {
			continue
		}

		for _, langDef := range i18n.Languages {
			fieldName := strings.Title(langDef.Code)
			text, ok := sec.StringValue(fieldName, "")
			if ok && text != "" {
				lp := p.pack(langDef.Code)
				lp.UI[code] = UITranslation{Code: code, Text: strings.TrimSpace(text)}
				counts[langDef.Code]++
			}
		}

		// 单独处理 piglatin
		if text, ok := sec.StringValue("piglatin", ""); ok && text != "" {
			lp := p.pack("piglatin")
			lp.UI[code] = UITranslation{Code: code, Text: strings.TrimSpace(text)}
			counts["piglatin"]++
		}
	}

	for lang, count := range counts {
		if count > 0 {
			fmt.Printf("[Translation]   - 从游戏本体提取到 [%s] UI 翻译 %d 项\n", lang, count)
		}
	}
}

func (p *TranslationPack) loadCampaignsAndMissions(lang, modName string) {
	lp := p.pack(lang)

	campsRoot := filepath.Join(lang, "loose", "translation/mods", modName, "camps")
	campaignsDir := filepath.Join(campsRoot, "camps")

	briefsDir := filepath.Join(campsRoot, "briefs")
	if entries, err := p.transFS.ReadDir(briefsDir); err == nil {
		for _, e := range entries {
			if e.IsDir || !strings.HasSuffix(strings.ToLower(e.Path), ".txt") {
				continue
			}
			if data, err := p.transFS.ReadFileLimit(e.Path, 4<<20); err == nil {
				key := strings.ToLower(strings.TrimSuffix(filepath.Base(e.Path), filepath.Ext(e.Path)))
				lp.Briefs[key] = BriefTranslation{Name: key, Text: data}
			}
		}
	}

	missionNames := make(map[string]string)
	missionsPath := filepath.Join(campsRoot, "missions.tdf")
	if data, err := p.transFS.ReadFileLimit(missionsPath, 1<<20); err == nil {
		if doc, err := formats.ParseTDF(data); err == nil && doc.Root != nil {
			for _, sec := range doc.Root.Sections() {
				key := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(sec.Name), ".ota"))
				if name, ok := sec.StringValue("missionname", ""); ok && name != "" {
					missionNames[key] = strings.TrimSpace(name)
				}
			}
		}
	}

	camplistPath := filepath.Join(campaignsDir, "camplist.tdf")
	if data, err := p.transFS.ReadFileLimit(camplistPath, 1<<20); err == nil {
		if doc, err := formats.ParseTDF(data); err == nil && doc.Root != nil {
			for _, sec := range doc.Root.Sections() {
				campCode := strings.TrimSpace(sec.Name)
				if name, ok := sec.StringValue("campaignname", ""); ok && name != "" {
					lp.Campaigns[campCode] = CampaignTranslation{
						Code: campCode,
						Name: strings.TrimSpace(name),
					}
				}
			}
		}
	}

	entries, err := p.transFS.ReadDir(campaignsDir)
	if err != nil {
		return
	}

	processTDF := func(data []byte, fileName string) {
		doc, err := formats.ParseTDF(data)
		if err != nil || doc.Root == nil {
			return
		}

		for _, sec := range doc.Root.Sections() {
			var mFile, mName string
			if strings.HasPrefix(strings.ToUpper(sec.Name), "MISSION") {
				mName, _ = sec.StringValue("missionname", "")
				mFile, _ = sec.StringValue("missionfile", "")
			} else if strings.HasSuffix(strings.ToLower(sec.Name), ".ota") {
				mFile = sec.Name
				mName, _ = sec.StringValue("missionname", "")
			}

			mFile = strings.TrimSpace(mFile)
			if mFile == "" {
				continue
			}

			otaKey := strings.ToLower(strings.TrimSuffix(mFile, ".ota"))

			translatedName := mName
			if overrideName, ok := missionNames[otaKey]; ok {
				translatedName = overrideName
			}

			var briefing []byte
			briefName := p.extractBriefName(mFile)
			if briefName == "" {
				briefName = otaKey
			}
			briefKey := strings.ToLower(strings.TrimSuffix(filepath.Base(briefName), filepath.Ext(briefName)))
			if b, ok := lp.Briefs[briefKey]; ok {
				briefing = b.Text
			}

			lp.Missions[otaKey] = MissionTranslation{
				Code:         mFile,
				Name:         strings.TrimSpace(translatedName),
				CampaignCode: fileName,
				Briefing:     briefing,
			}
		}
	}

	for _, e := range entries {
		if e.IsDir || !strings.HasSuffix(strings.ToLower(e.Path), ".tdf") {
			continue
		}
		baseName := filepath.Base(e.Path)
		if strings.EqualFold(baseName, "camplist.tdf") {
			continue
		}

		data, err := p.transFS.ReadFileLimit(e.Path, 4<<20)
		if err != nil {
			continue
		}
		fileName := strings.TrimSuffix(baseName, filepath.Ext(baseName))
		processTDF(data, fileName)
	}
}

func (p *TranslationPack) extractBriefName(otaFile string) string {
	if otaFile == "" || p.gameFS == nil {
		return ""
	}

	otaPath := filepath.Join("maps", otaFile)
	data, err := p.gameFS.ReadFileLimit(otaPath, 4<<20)
	if err != nil {
		return ""
	}

	doc, err := formats.ParseTDF(data)
	if err != nil || doc.Root == nil {
		return ""
	}

	header := doc.Root.Section("GlobalHeader")
	if header == nil {
		header = doc.Root.Section("HEADER")
	}
	if header != nil {
		if brief, ok := header.StringValue("brief", ""); ok && brief != "" {
			return strings.TrimSpace(brief)
		}
	}

	return ""
}

func (p *TranslationPack) loadUITranslations(lang, modName string) {
	path := filepath.Join(lang, "loose", "translation/mods", modName, "translate.tdf")
	data, err := p.transFS.ReadFileLimit(path, 4<<20)
	if err != nil {
		return
	}

	doc, err := formats.ParseTDF(data)
	if err != nil || doc.Root == nil {
		return
	}

	lp := p.pack(lang)
	count := 0
	for _, section := range doc.Root.Sections() {
		code := section.OriginalName
		text, hasText := section.StringValue("translation", "")

		if code != "" && hasText && text != "" {
			code = strings.TrimSpace(code)
			text = strings.TrimSpace(text)

			lp.UI[code] = UITranslation{
				Code: code,
				Text: text,
			}
			count++
		}
	}
	if count > 0 {
		fmt.Printf("[Translation]   - [%s] 载入 UI 翻译 %d 项 (%s)\n", lang, count, path)
	}
}

// NewTranslationTable 构造一个正确的翻译表。
func newTranslationTable(entries map[string]string) *TranslationTable {
	// 1. 收集所有的 source，去重
	sources := make([]string, 0, len(entries))
	for src := range entries {
		sources = append(sources, src)
	}

	// 2. 按 source 的字节序排序 (byte-sorted source order)
	sort.Strings(sources)

	// 3. 按排序后的顺序组装 entries
	t := &TranslationTable{
		entries: make([]translationEntry, 0, len(sources)),
	}
	for _, src := range sources {
		t.entries = append(t.entries, translationEntry{
			source:      src,
			translation: entries[src],
		})
	}
	return t
}

func ApplyTranslationsTable(table *TranslationTable) {
	kv := make(map[string]string)
	for _, entry := range GlobalTranslations.Packs[i18n.Language()].UI {
		kv[entry.Code] = entry.Text
	}
	table = newTranslationTable(kv)
}

// GetFeature 查询 feature 翻译，带 English 回退
func (p *TranslationPack) GetFeature(lang, featureKey string) (UITranslation, bool) {
	if p == nil {
		return UITranslation{}, false
	}
	key := strings.TrimSpace(featureKey)

	// 1. 优先查指定语言
	lp, ok := p.Packs[strings.ToLower(strings.TrimSpace(lang))]
	if ok {
		if t, found := lp.Features[key]; found {
			return t, true
		}
	}

	// 2. 回退到 english
	if !strings.EqualFold(lang, "english") {
		if enLp, ok := p.Packs["english"]; ok {
			if t, found := enLp.Features[key]; found {
				return t, true
			}
		}
	}

	return UITranslation{}, false
}

// loadFeatureTranslations 加载 features 翻译
// 约定格式：
// [some_feature]
//
//	{
//	    description=Wreckage;
//	    translation=残骸;
//	}
func (p *TranslationPack) loadFeatureTranslations(lang, modName string) {
	path := filepath.Join(lang, "loose", "translation/mods", modName, "features.tdf")
	data, err := p.transFS.ReadFileLimit(path, 4<<20)
	if err != nil {
		return
	}

	doc, err := formats.ParseTDF(data)
	if err != nil || doc.Root == nil {
		return
	}

	lp := p.pack(lang)
	count := 0
	for _, section := range doc.Root.Sections() {
		code := section.OriginalName
		trans, hasTrans := section.StringValue("translation", "")

		code = strings.TrimSpace(code)
		trans = strings.TrimSpace(trans)
		if code == "" || !hasTrans || trans == "" {
			continue
		}

		lp.Features[code] = UITranslation{
			Code: code,
			Text: trans,
		}
		count++
	}
	if count > 0 {
		fmt.Printf("[Translation]   - [%s] 载入 Feature 翻译 %d 项 (%s)\n", lang, count, path)
	}
}

func ApplyFeatureText(cat *Catalog) {
	for i, entry := range cat.Features {
		if t, ok := GlobalTranslations.GetFeature(i18n.Language(), entry.Description); ok {
			cat.Features[i].Description = t.Text
		}
	}
}

func ApplyUnitText(cat *Catalog) {
	var machedUnit, mismachedUnit, newUnit int
	for i, entry := range cat.unitRecords {
		if t, ok := GlobalTranslations.GetUnit(i18n.Language(), entry.UnitName); ok {
			if cat.unitRecords[i].Name == t.NameFrom && cat.unitRecords[i].Description == t.DescriptionFrom {
				cat.unitRecords[i].Name = t.Name + "[" + cat.unitRecords[i].Name + "]"
				cat.unitRecords[i].Description = t.Description
				machedUnit++
			} else {
				//when mod applied,different translation from loaded units print for translate work
				fmt.Println("unitKey:", cat.unitRecords[i].UnitName, ",unitName:", cat.unitRecords[i].Name, ",unitDescription:", cat.unitRecords[i].Description)
				cat.unitRecords[i].Name = t.Name + "[" + cat.unitRecords[i].Name + "]"
				cat.unitRecords[i].Description = t.Description
				mismachedUnit++
			}
		} else {
			//when mod applied,different translation from loaded units print for translate work
			fmt.Println("unitKey:", cat.unitRecords[i].UnitName, ",unitName:", cat.unitRecords[i].Name, ",unitDescription:", cat.unitRecords[i].Description)
			newUnit++
		}
	}
	fmt.Println(fmt.Sprintf("单位翻译中严格匹配的单位数：%d,未严格匹配数：%d,新增单位数：%d", machedUnit, mismachedUnit, newUnit))
}

// fillNativeBriefs 从 gameFS 的 camps/briefs 读取所有英文原版简报，
// 填进 lp.Briefs 作为底座。已存在的 key 不覆盖（语言目录后写会覆盖）。
// fillNativeBriefs 从 gameFS 的 camps/briefs 读取所有英文原版简报，
// 填进 lp.Briefs 作为底座。已存在的 key 不覆盖（语言目录后写会覆盖）。
func (p *TranslationPack) fillNativeBriefs(lang string, lp *LanguagePack) {
	if p.gameFS == nil || lp == nil {
		return
	}
	briefsDir := filepath.Join("camps", "briefs")
	entries, err := p.gameFS.ReadDir(briefsDir)
	if err != nil {
		return
	}
	count := 0
	for _, e := range entries {
		if e.IsDir || !strings.HasSuffix(strings.ToLower(e.Path), ".txt") {
			continue
		}
		key := strings.ToLower(strings.TrimSuffix(filepath.Base(e.Path), filepath.Ext(e.Path)))
		if key == "" {
			continue
		}
		if _, exists := lp.Briefs[key]; exists {
			continue
		}
		data, err := p.gameFS.ReadFileLimit(e.Path, 4<<20)
		if err != nil {
			continue
		}
		lp.Briefs[key] = BriefTranslation{Name: key, Text: data}
		count++
	}
	if count > 0 {
		fmt.Printf("[Translation]   - [%s] 从游戏本体加载英文简报 %d 项\n", lang, count)
	}
}
