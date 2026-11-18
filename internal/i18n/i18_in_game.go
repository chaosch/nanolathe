package i18n

import (
	"fmt"
	"github.com/nanolathe-gg/nanolathe/formats"
	"github.com/nanolathe-gg/nanolathe/internal/content"
	"github.com/nanolathe-gg/nanolathe/vfs"
	"os"
	"path/filepath"
	"strings"
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

type CampaignTranslation struct {
	Code string // 英文 Code / 文件名 (作为 Map 的 Key)
	Name string // 译文（如 "遭遇柯古斯"）
}

type MissionTranslation struct {
	Code         string // 任务英文原名
	Name         string // 任务译文名
	File         string // 任务文件 (如 "Three Passes.ota")
	CampaignCode string // 所属战役的 Code (英文文件名)
	Briefing     []byte // 简报全文 (找不到就是 nil)
}

// 新增：完全独立出来的 Brief 结构
type BriefTranslation struct {
	Name string // txt 文件名 (不带后缀，作为 Map 的 Key)
	Text []byte // 译文纯文本
}

type UITranslation struct {
	Code string
	Text string
}

type TranslationPack struct {
	looseDir string
	modeName string

	transFS *vfs.FS   // 自己挂载的翻译包 FS (指针类型)
	gameFS  vfs.FSOps // 外部传进来的游戏本体只读 FS

	Briefs    map[string]BriefTranslation // Key: txt 文件名 (无后缀、全小写)
	Units     map[string]UnitTranslation
	Missions  map[string]MissionTranslation
	Campaigns map[string]CampaignTranslation
	UI        map[string]UITranslation
	Full      bool
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
	p.Full = false
}

// Init 挂载 VFS 并保存环境
func (p *TranslationPack) Init(looseDir string, sel string, gameFS vfs.FSOps) error {
	p.Clear()
	p.looseDir = looseDir
	p.modeName = sel
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
func (p *TranslationPack) LoadAll(looseDir string, sel string, gameFS vfs.FSOps) {
	if err := p.Init(looseDir, sel, gameFS); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return
	}
	defer p.Close()

	fmt.Printf("[Translation] 开始加载底座翻译 (ota)\n")
	p.loadAllForMod("ota")

	if p.modeName != "" {
		modName := p.modeName
		if modName != "" && modName != "ota" {
			fmt.Printf("[Translation] 开始加载 Mod 覆盖翻译 (%s)\n", modName)
			p.loadAllForMod(modName)
		}
	}

	fmt.Printf("[Translation] 加载完毕。统计: 单位 %d 个, 任务 %d 个, 战役 %d 个, UI %d 项\n",
		len(p.Units), len(p.Missions), len(p.Campaigns), len(p.UI))
	for _, c := range p.Missions {
		fmt.Println(c.Code, c.Name)
	}
	p.Full = true
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
	if p.modeName != "" {
		modName := p.modeName
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
	if p.modeName != "" {
		modName := p.modeName
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
	if p.modeName != "" {
		modName := p.modeName
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
	campsRoot := filepath.Join("translation/mods", modName, "camps")
	campaignsDir := filepath.Join(campsRoot, "camps")

	// ==========================================
	// 1. 读取 briefs (纯文本)
	// ==========================================
	p.Briefs = make(map[string]BriefTranslation)
	briefsDir := filepath.Join(campsRoot, "briefs")
	if entries, err := p.transFS.ReadDir(briefsDir); err == nil {
		for _, e := range entries {
			if e.IsDir || !strings.HasSuffix(strings.ToLower(e.Path), ".txt") {
				continue
			}
			if data, err := p.transFS.ReadFileLimit(e.Path, 4<<20); err == nil {
				key := strings.ToLower(strings.TrimSuffix(filepath.Base(e.Path), filepath.Ext(e.Path)))
				p.Briefs[key] = BriefTranslation{Name: key, Text: data}
			}
		}
	}

	// ==========================================
	// 2. 读取 missions.tdf
	// ==========================================
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

	// ==========================================
	// 3. 核心：优先读取 camplist.tdf 构建 Campaigns
	// ==========================================
	p.Campaigns = make(map[string]CampaignTranslation) // 清空确保干净
	camplistPath := filepath.Join(campaignsDir, "camplist.tdf")
	fmt.Printf("[Translation] 正在加载战役列表: %s\n", camplistPath)

	if data, err := p.transFS.ReadFileLimit(camplistPath, 1<<20); err == nil {
		if doc, err := formats.ParseTDF(data); err == nil && doc.Root != nil {
			for _, sec := range doc.Root.Sections() {
				campCode := strings.TrimSpace(sec.Name)
				if name, ok := sec.StringValue("campaignname", ""); ok && name != "" {
					p.Campaigns[campCode] = CampaignTranslation{
						Code: campCode,
						Name: strings.TrimSpace(name),
					}
				}
			}
			fmt.Printf("[Translation] 成功从 camplist.tdf 解析了 %d 个战役\n", len(p.Campaigns))
		} else {
			fmt.Printf("[Translation] 解析 camplist.tdf 失败: %v\n", err)
		}
	} else {
		fmt.Printf("[Translation] 找不到 camplist.tdf: %v\n", err)
	}

	// ==========================================
	// 4. 遍历其他 TDF 组装 Missions（不再覆盖 Campaigns）
	// ==========================================
	entries, err := p.transFS.ReadDir(campaignsDir)
	if err != nil {
		return
	}

	processTDF := func(data []byte, fileName string) {
		doc, err := formats.ParseTDF(data)
		if err != nil || doc.Root == nil {
			return
		}

		// 组装 Missions
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

			// 任务名覆盖
			translatedName := mName
			if overrideName, ok := missionNames[otaKey]; ok {
				translatedName = overrideName
			}

			// 提取简报
			var briefing []byte
			briefName := p.extractBriefName(mFile)
			if briefName == "" {
				briefName = otaKey
			}
			briefKey := strings.ToLower(strings.TrimSuffix(filepath.Base(briefName), filepath.Ext(briefName)))
			if b, ok := p.Briefs[briefKey]; ok {
				briefing = b.Text
			}

			p.Missions[otaKey] = MissionTranslation{
				Code:         mFile,
				Name:         strings.TrimSpace(translatedName),
				CampaignCode: fileName, // 当前遍历的战役文件名
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
			continue // 跳过它，因为上面已经读了
		}

		data, err := p.transFS.ReadFileLimit(e.Path, 4<<20)
		if err != nil {
			continue
		}
		fileName := strings.TrimSuffix(baseName, filepath.Ext(baseName))
		processTDF(data, fileName)
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

// 1. 查战役翻译
// 传入如 "Krogoth Encounter" 或 "Krogoth Encounter.tdf"
func (p *TranslationPack) GetCampaign(code string) (CampaignTranslation, bool) {
	if p == nil || code == "" {
		return CampaignTranslation{}, false
	}
	// 去除可能带上的 .tdf 后缀，并处理前后空格
	key := strings.TrimSpace(code)
	key = strings.TrimSuffix(key, ".tdf")

	c, ok := p.Campaigns[key]
	return c, ok
}

// 2. 查任务翻译
// 传入如 "Krogoth Encounter.ota" 或 "Krogoth Encounter"
func (p *TranslationPack) GetMission(otaFile string) (MissionTranslation, bool) {
	if p == nil || otaFile == "" {
		return MissionTranslation{}, false
	}
	// 统一转化为小写，且去掉 .ota 后缀
	key := strings.ToLower(strings.TrimSpace(otaFile))
	key = strings.TrimSuffix(key, ".ota")

	m, ok := p.Missions[key]
	return m, ok
}

// 3. 查简报翻译（单独抽离的）
// 传入如 "Krogoth" 或 "Krogoth.txt"
func (p *TranslationPack) GetBrief(briefName string) (BriefTranslation, bool) {
	if p == nil || briefName == "" {
		return BriefTranslation{}, false
	}
	// 统一转化为小写，且去掉 .txt 后缀
	key := strings.ToLower(strings.TrimSpace(briefName))
	key = strings.TrimSuffix(key, ".txt")

	b, ok := p.Briefs[key]
	return b, ok
}

func LoadTranslationTableDispatcher(looseDir string, modName string, gameFS vfs.FSOps, language string) (*content.TranslationTable, error) {
	if LanguageNeedCJK(language) {
		if !GlobalTranslations.Full {
			GlobalTranslations.LoadAll(looseDir, modName, gameFS)
		}
		return GlobalTranslations.AsTranslationTable()
	} else {
		return content.LoadTranslationTable(gameFS, "")
	}
}
