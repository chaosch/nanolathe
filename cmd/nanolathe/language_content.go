package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/vfs"
)

// ============================================================================
// 数据结构
// ============================================================================

type UnitTranslation struct {
	Name            string
	Description     string
	NameFrom        string
	DescriptionFrom string
}

type MissionTranslation struct {
	Name         string // 任务译文名
	Code         string // 任务英文原名
	File         string // 任务文件 (如 "Three Passes.ota")
	CampaignCode string // 所属战役的 Code (英文文件名)
	Briefing     []byte // 简报全文 (找不到就是 nil)
}

type CampaignTranslation struct {
	Name string // 战役译文名 (读不到就是空串)
	Code string // 英文文件名 (不含扩展名)
}

type UITranslation struct {
	Code string
	Text string
}

type TranslationPack struct {
	looseDir string
	sel      modSelection

	transFS *vfs.FS   // 自己挂载的翻译包 FS (指针类型)
	gameFS  vfs.FSOps // 外部传进来的游戏本体只读 FS

	Units     map[string]UnitTranslation
	Missions  map[string]MissionTranslation
	Campaigns map[string]CampaignTranslation
	UI        map[string]UITranslation
}

var GlobalTranslations = &TranslationPack{
	Units:     make(map[string]UnitTranslation),
	Missions:  make(map[string]MissionTranslation),
	Campaigns: make(map[string]CampaignTranslation),
	UI:        make(map[string]UITranslation),
}

// ============================================================================
// 查询方法
// ============================================================================

func (p *TranslationPack) GetUnit(unitKey string) (UnitTranslation, bool) {
	t, ok := p.Units[content.CanonicalKey(unitKey)]
	return t, ok
}

func (p *TranslationPack) GetMission(missionFile string) (MissionTranslation, bool) {
	key := strings.ToLower(strings.TrimSpace(missionFile))
	m, ok := p.Missions[key]
	return m, ok
}

func (p *TranslationPack) GetCampaign(code string) (CampaignTranslation, bool) {
	c, ok := p.Campaigns[code]
	return c, ok
}

func (p *TranslationPack) GetUI(code string) (UITranslation, bool) {
	code = strings.TrimSpace(code)
	u, ok := p.UI[code]
	return u, ok
}

// ============================================================================
// 加载环境管理
// ============================================================================

func (p *TranslationPack) Clear() {
	p.Units = make(map[string]UnitTranslation)
	p.Missions = make(map[string]MissionTranslation)
	p.Campaigns = make(map[string]CampaignTranslation)
	p.UI = make(map[string]UITranslation)
}

// Init 挂载 VFS 并保存环境
func (p *TranslationPack) Init(looseDir string, sel modSelection, gameFS vfs.FSOps) error {
	p.Clear()
	p.looseDir = looseDir
	p.sel = sel
	p.gameFS = gameFS

	fmt.Printf("[Translation] 开始挂载翻译包: %s\n", looseDir)

	tmp := vfs.New()
	if err := tmp.MountDirectory(looseDir, 5000); err != nil {
		tmp.Close()
		return fmt.Errorf("[Translation] 挂载失败: %w", err)
	}
	p.transFS = tmp
	return nil
}

// Close 卸载翻译包环境
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

// LoadAll 保持原本的参数签名，内部自动挂载、加载并卸载
func (p *TranslationPack) LoadAll(looseDir string, sel modSelection, gameFS vfs.FSOps) {
	if err := p.Init(looseDir, sel, gameFS); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return
	}
	defer p.Close()

	fmt.Printf("[Translation] 开始加载底座翻译 (ota)\n")
	p.loadAllForMod("ota")

	if p.sel.mod != nil {
		modName := p.sel.mod.ID
		if modName != "" && modName != "ota" {
			fmt.Printf("[Translation] 开始加载 Mod 覆盖翻译 (%s)\n", modName)
			p.loadAllForMod(modName)
		}
	}

	fmt.Printf("[Translation] 加载完毕。统计: 单位 %d 个, 任务 %d 个, 战役 %d 个, UI %d 项\n",
		len(p.Units), len(p.Missions), len(p.Campaigns), len(p.UI))
}

// ============================================================================
// 分步加载（需先调用 Init，最后调用 Close）
// ============================================================================

// LoadUnitsOnly 仅加载单位翻译
func (p *TranslationPack) LoadUnitsOnly() {
	if p.transFS == nil {
		return
	}
	p.loadUnits("ota")
	if p.sel.mod != nil {
		modName := p.sel.mod.ID
		if modName != "" && modName != "ota" {
			p.loadUnits(modName)
		}
	}
}

// LoadCampaignsOnly 仅加载战役与任务翻译
func (p *TranslationPack) LoadCampaignsOnly() {
	if p.transFS == nil {
		return
	}
	p.loadCampaignsAndMissions("ota")
	if p.sel.mod != nil {
		modName := p.sel.mod.ID
		if modName != "" && modName != "ota" {
			p.loadCampaignsAndMissions(modName)
		}
	}
}

// LoadUIOnly 仅加载 UI 翻译
func (p *TranslationPack) LoadUIOnly() {
	if p.transFS == nil {
		return
	}
	p.loadUITranslations("ota")
	if p.sel.mod != nil {
		modName := p.sel.mod.ID
		if modName != "" && modName != "ota" {
			p.loadUITranslations(modName)
		}
	}
}

// ============================================================================
// 内部解析方法
// ============================================================================

func (p *TranslationPack) loadAllForMod(modName string) {
	p.loadUnits(modName)
	p.loadCampaignsAndMissions(modName)
	p.loadUITranslations(modName)
}

func (p *TranslationPack) loadUnits(modName string) {
	path := filepath.Join("translation/mods", modName, "units.tdf")
	data, err := p.transFS.ReadFileLimit(path, 4<<20)
	if err != nil {
		return
	}

	doc, err := formats.ParseTDF(data)
	if err != nil {
		return
	}

	count := 0
	for _, section := range doc.Root.Sections() {
		unitKey := content.CanonicalKey(section.OriginalName)
		if unitKey == "" {
			continue
		}

		name, _ := section.StringValue("name", "")
		description, _ := section.StringValue("description", "")
		nameFrom, _ := section.StringValue("name-from", "")
		descriptionFrom, _ := section.StringValue("description-from", "")

		p.Units[unitKey] = UnitTranslation{
			Name:            strings.TrimSpace(name),
			Description:     strings.TrimSpace(description),
			NameFrom:        strings.TrimSpace(nameFrom),
			DescriptionFrom: strings.TrimSpace(descriptionFrom),
		}
		count++
	}
	if count > 0 {
		fmt.Printf("[Translation]   - 载入单位翻译 %d 项 (%s)\n", count, path)
	}
}

func (p *TranslationPack) loadCampaignsAndMissions(modName string) {
	// 基础目录 translation/mods/<mod>/camps
	campsRoot := filepath.Join("translation/mods", modName, "camps")

	// 1. 读取 briefs 目录 (在 campsRoot 下)
	briefFiles := make(map[string][]byte)
	briefsDir := filepath.Join(campsRoot, "briefs")
	if entries, err := p.transFS.ReadDir(briefsDir); err == nil {
		for _, e := range entries {
			if e.IsDir || !strings.HasSuffix(strings.ToLower(e.Path), ".txt") {
				continue
			}
			if data, err := p.transFS.ReadFileLimit(e.Path, 4<<20); err == nil {
				baseName := strings.TrimSuffix(filepath.Base(e.Path), filepath.Ext(e.Path))
				briefFiles[strings.ToLower(baseName)] = bytes.TrimSpace(data)
			}
		}
		if len(briefFiles) > 0 {
			fmt.Printf("[Translation]   - 载入简报文本 %d 份 (%s)\n", len(briefFiles), briefsDir)
		}
	}

	// 2. 读取 missions.tdf (在 campsRoot 下)，用于任务名覆盖
	missionNames := make(map[string]string)
	missionsPath := filepath.Join(campsRoot, "missions.tdf")
	if data, err := p.transFS.ReadFileLimit(missionsPath, 1<<20); err == nil {
		if doc, err := formats.ParseTDF(data); err == nil && doc.Root != nil {
			for _, sec := range doc.Root.Sections() {
				key := strings.ToLower(strings.TrimSpace(sec.Name))
				if name, ok := sec.StringValue("missionname", ""); ok && name != "" {
					missionNames[key] = name
				}
			}
			if len(missionNames) > 0 {
				fmt.Printf("[Translation]   - 载入任务名覆盖表 %d 项 (%s)\n", len(missionNames), missionsPath)
			}
		}
	}

	// 3. 读取 camps/camps 目录下的战役骨架 .tdf
	campaignsDir := filepath.Join(campsRoot, "camps")
	entries, err := p.transFS.ReadDir(campaignsDir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir || !strings.HasSuffix(strings.ToLower(e.Path), ".tdf") {
			continue
		}

		data, err := p.transFS.ReadFileLimit(e.Path, 4<<20)
		if err != nil {
			continue
		}

		doc, err := formats.ParseTDF(data)
		if err != nil || doc.Root == nil {
			continue
		}

		// 文件名（不含扩展名）作为 Code
		fileName := strings.TrimSuffix(filepath.Base(e.Path), filepath.Ext(e.Path))

		// A. 提取战役名
		campName := ""
		header := doc.Root.Section("HEADER")
		if header != nil {
			if name, ok := header.StringValue("campaignname", ""); ok && name != "" {
				campName = strings.TrimSpace(name)
			}
		}

		// 存入 Campaigns Map
		p.Campaigns[fileName] = CampaignTranslation{
			Name: campName,
			Code: fileName,
		}

		// B. 组装 Missions
		for _, sec := range doc.Root.Sections() {
			secName := strings.ToUpper(sec.Name)
			if !strings.HasPrefix(secName, "MISSION") {
				continue
			}

			mEnName, _ := sec.StringValue("missionname", "")
			mFile, _ := sec.StringValue("missionfile", "") // 如 "Three Passes.ota"
			mFileKey := strings.ToLower(strings.TrimSpace(mFile))

			// 任务名查找：优先看 missions.tdf 里有没有覆盖
			translatedMissionName := mEnName
			if overrideName, ok := missionNames[mFileKey]; ok {
				translatedMissionName = overrideName
			}

			var briefing []byte

			// 从 gameFS 的 maps 目录直接读 .ota 提取 brief 文件名
			briefName := p.extractBriefName(mFile)
			if briefName != "" {
				briefKey := strings.ToLower(strings.TrimSuffix(briefName, filepath.Ext(briefName)))
				// 只在当前 modName 的 briefs 里找，找不到就为 nil
				briefing = briefFiles[briefKey]
			}

			p.Missions[mFileKey] = MissionTranslation{
				Name:         translatedMissionName,
				Code:         mEnName,
				File:         mFile,
				CampaignCode: fileName, // 记录来源战役
				Briefing:     briefing,
			}
		}
	}
}

// extractBriefName 直接从 gameFS 的 maps 目录读取 .ota 并提取 brief 字段
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

func (p *TranslationPack) loadUITranslations(modName string) {
	path := filepath.Join("translation/mods", modName, "translate.tdf")
	data, err := p.transFS.ReadFileLimit(path, 4<<20)
	if err != nil {
		return
	}

	doc, err := formats.ParseTDF(data)
	if err != nil || doc.Root == nil {
		return
	}

	count := 0
	for _, section := range doc.Root.Sections() {
		code := section.OriginalName
		text, hasText := section.StringValue("chinese", "")

		if code != "" && hasText && text != "" {
			code = strings.TrimSpace(code)
			text = strings.TrimSpace(text)

			p.UI[code] = UITranslation{
				Code: code,
				Text: text,
			}
			fmt.Println(fmt.Sprintf("【原始文本 Hex】: %xs\n", text))

			fmt.Println("【原始文本 string】: ", text)
			count++
		}
	}
	if count > 0 {
		fmt.Printf("[Translation]   - 载入 UI 翻译 %d 项 (%s):\n", count, path)
	}
}

// AsTranslationTable 将已加载的 UI 翻译转换为 content.TranslationTable 格式
func (p *TranslationPack) AsTranslationTable() (*content.TranslationTable, error) {
	if len(p.UI) == 0 {
		return nil, fmt.Errorf("no UI translations loaded")
	}

	// 构造 source -> translation 的 map
	entries := make(map[string]string, len(p.UI))
	for code, ui := range p.UI {
		entries[code] = ui.Text
	}

	return content.NewTranslationTable(entries), nil
}
