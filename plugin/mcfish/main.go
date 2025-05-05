// Package mcfish 钓鱼模拟器
package mcfish

import (
	"encoding/json"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	fcext "github.com/FloatTech/floatbox/ctxext"
	"github.com/FloatTech/floatbox/math"
	sql "github.com/FloatTech/sqlite"
	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
	"github.com/FloatTech/zbputils/ctxext"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

type fishdb struct {
	sync.RWMutex
	db sql.Sqlite
}

// FishLimit 钓鱼次数上限
const FishLimit = 2000

// version 规则版本号
const version = "5.6.2"

// 各物品信息
type jsonInfo struct {
	ZoneInfo    []zoneInfo    `json:"分类"` // 区域概率
	ArticleInfo []articleInfo `json:"物品"` // 物品信息
}
type zoneInfo struct {
	Name        string `json:"类型"`        // 类型
	Probability int    `json:"概率[0-100)"` // 概率
}
type articleInfo struct {
	Name        string `json:"名称"`                  // 名称
	Type        string `json:"类型"`                  // 类型
	Probability int    `json:"概率[0-100),omitempty"` // 概率
	Durable     int    `json:"耐久上限,omitempty"`      // 耐久
	Price       int    `json:"价格"`                  // 价格
}

type probabilityLimit struct {
	Min int
	Max int
}

type equip struct {
	ID          int64  // 用户
	Equip       string // 装备名称
	Durable     int    // 耐久
	Maintenance int    // 维修次数
	Induce      int    // 诱钓等级
	Favor       int    // 眷顾等级
	Durability  int    // 耐久附魔等级
	ExpRepair   int    // 经验修补附魔等级
}

type article struct {
	Duration int64
	Name     string
	Number   int
	Other    string // 耐久/维修次数/诱钓/眷顾
	Type     string
}

type store struct {
	Duration int64
	Name     string
	Number   int
	Price    int
	Other    string // 耐久/维修次数/诱钓/眷顾
	Type     string
}

type fishState struct {
	ID       int64
	Duration int64
	Fish     int
	Equip    int
	Curse    int // 功德--(x)
	Bless    int // 功德++(x)
}

type storeDiscount struct {
	Name     string
	Discount int
}

// buff状态记录
// buff0: 优惠卷
type buffInfo struct {
	ID        int64
	Duration  int64
	BuyTimes  int `db:"Buff0"` // 购买次数
	Coupon    int `db:"Buff1"` // 优惠卷
	SalesPole int `db:"Buff2"` // 卖鱼竿上限
	BuyTing   int `db:"Buff3"` // 购买上限
	Buff4     int `db:"Buff4"` // 暂定
	Buff5     int `db:"Buff5"` // 暂定
	Buff6     int `db:"Buff6"` // 暂定
	Buff7     int `db:"Buff7"` // 暂定
	Buff8     int `db:"Buff8"` // 暂定
	Buff9     int `db:"Buff9"` // 暂定
}

var (
	articlesInfo  = jsonInfo{}                            // 物品信息
	thingList     = make([]string, 0, 100)                // 竿列表
	poleList      = make([]string, 0, 10)                 // 竿列表
	fishList      = make([]string, 0, 10)                 // 鱼列表
	treasureList  = make([]string, 0, 10)                 // 鱼列表
	wasteList     = make([]string, 0, 10)                 // 垃圾列表
	probabilities = make(map[string]probabilityLimit, 50) // 概率分布
	priceList     = make(map[string]int, 50)              // 价格分布
	durationList  = make(map[string]int, 50)              // 装备耐久分布
	discountList  = make(map[string]int, 50)              // 价格波动信息
	enchantLevel  = []string{"0", "Ⅰ", "Ⅱ", "Ⅲ"}
	dbdata        fishdb
)

var (
	engine = control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault: false,
		Brief:            "钓鱼",
		Help: "一款钓鱼模拟器,规则:V" + version +
			"\n----------指令----------\n" +
			"- 钓鱼背包\n" +
			"- 进行钓鱼 / 进行n次钓鱼\n" +
			"- 修复鱼竿\n" +
			"- 钓鱼商店 / 钓鱼看板\n" +
			"- 购买xxx / 购买xxx [数量]\n- 出售xxx / 出售xxx [数量]\n" +
			"- 消除[绑定|宝藏]诅咒 / 消除[绑定|宝藏]诅咒 [数量]\n" +
			"- 装备[xx竿|三叉戟|美西螈]\n" +
			"- 附魔[诱钓|海之眷顾|耐久|经验修补]\n" +
			"- 合成[xx竿|三叉戟]\n" +
			"- 出售所有垃圾\n" +
			"- 当前装备概率明细\n" +
			"- 查看钓鱼规则\n",
		PublicDataFolder: "McFish",
	}).ApplySingle(ctxext.DefaultSingle)
	getdb = fcext.DoOnceOnSuccess(func(ctx *zero.Ctx) bool {
		dbdata.db = sql.New(engine.DataFolder() + "fishdata.db")
		err := dbdata.db.Open(time.Hour * 24)
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at main.go.1]:", err))
			return false
		}

		// 检查并升级数据库表结构
		err = upgradeEquipsTable(ctx)
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at main.go.2]:", err))
			return false
		}

		return true
	})
)

// 升级数据库表结构，添加新字段
func upgradeEquipsTable(ctx *zero.Ctx) error {
	// 检查equips表是否存在
	if !dbdata.db.CanFind("sqlite_master", "WHERE type='table' AND name='equips'") {
		return nil // 表不存在，不需要升级
	}

	// 创建一个临时结构体，用于检查表结构
	type tempEquip struct {
		ID          int64
		Equip       string
		Durable     int
		Maintenance int
		Induce      int
		Favor       int
		Durability  int
		ExpRepair   int
	}

	// 尝试创建临时结构体
	var temp tempEquip
	err := dbdata.db.Create("equips", &temp)
	if err != nil {
		// 如果创建失败，可能是因为表结构不匹配
		// 我们需要修改表结构
		ctx.SendChain(message.Text("检测到mcfish插件数据库需要升级，正在升级..."))

		// 创建一个新表，包含所有字段
		type oldEquip struct {
			ID          int64
			Equip       string
			Durable     int
			Maintenance int
			Induce      int
			Favor       int
		}

		// 获取旧表数据
		var oldEquips []oldEquip
		var oldEquipTemp oldEquip
		err = dbdata.db.FindFor("equips", &oldEquipTemp, "", func() error {
			oldEquips = append(oldEquips, oldEquipTemp)
			return nil
		})
		if err != nil {
			return err
		}

		// 删除旧表
		err = dbdata.db.Drop("equips")
		if err != nil {
			return err
		}

		// 创建新表
		err = dbdata.db.Create("equips", &tempEquip{})
		if err != nil {
			return err
		}

		// 迁移数据
		for _, old := range oldEquips {
			newEquip := tempEquip{
				ID:          old.ID,
				Equip:       old.Equip,
				Durable:     old.Durable,
				Maintenance: old.Maintenance,
				Induce:      old.Induce,
				Favor:       old.Favor,
				Durability:  0,
				ExpRepair:   0,
			}
			err = dbdata.db.Insert("equips", &newEquip)
			if err != nil {
				return err
			}
		}

		ctx.SendChain(message.Text("mcfish插件数据库升级完成！"))
	}

	return nil
}

func init() {
	// go func() {
	_, err := engine.GetLazyData("articlesInfo.json", false)
	if err != nil {
		panic(err)
	}
	reader, err := os.Open(engine.DataFolder() + "articlesInfo.json")
	if err == nil {
		err = json.NewDecoder(reader).Decode(&articlesInfo)
	}
	if err == nil {
		err = reader.Close()
	}
	if err != nil {
		panic(err)
	}
	probableList := make([]int, 4)
	for _, info := range articlesInfo.ZoneInfo {
		switch info.Name {
		case "treasure":
			probableList[0] = info.Probability
		case "pole":
			probableList[1] = info.Probability
		case "fish":
			probableList[2] = info.Probability
		case "waste":
			probableList[3] = info.Probability
		}
	}
	probabilities["treasure"] = probabilityLimit{
		Min: 0,
		Max: probableList[0],
	}
	probabilities["pole"] = probabilityLimit{
		Min: probableList[0],
		Max: probableList[1],
	}
	probabilities["fish"] = probabilityLimit{
		Min: probableList[1],
		Max: probableList[2],
	}
	probabilities["waste"] = probabilityLimit{
		Min: probableList[2],
		Max: probableList[3],
	}
	minMap := make(map[string]int, 4)
	for _, info := range articlesInfo.ArticleInfo {
		switch {
		case info.Type == "pole" || info.Name == "美西螈":
			poleList = append(poleList, info.Name)
		case info.Type == "fish" || info.Name == "海豚":
			fishList = append(fishList, info.Name)
		case info.Type == "waste":
			wasteList = append(wasteList, info.Name)
		case info.Type == "treasure":
			treasureList = append(treasureList, info.Name)
		}
		if info.Name != "宝藏诅咒" {
			thingList = append(thingList, info.Name)
			priceList[info.Name] = info.Price
		}
		if info.Durable != 0 {
			durationList[info.Name] = info.Durable
		}
		probabilities[info.Name] = probabilityLimit{
			Min: minMap[info.Type],
			Max: minMap[info.Type] + info.Probability,
		}
		minMap[info.Type] += info.Probability
	}
	// }()
}

// 更新上限信息
func (sql *fishdb) updateFishInfo(uid int64, number int) (residue int, err error) {
	sql.Lock()
	defer sql.Unlock()
	userInfo := fishState{ID: uid}
	err = sql.db.Create("fishState", &userInfo)
	if err != nil {
		return 0, err
	}
	_ = sql.db.Find("fishState", &userInfo, "WHERE ID = ?", uid)
	if time.Unix(userInfo.Duration, 0).Day() != time.Now().Day() {
		userInfo.Fish = 0
		userInfo.Duration = time.Now().Unix()
	}
	if userInfo.Fish >= FishLimit {
		return 0, nil
	}
	residue = number
	if userInfo.Fish+number > FishLimit {
		residue = FishLimit - userInfo.Fish
		number = residue
	}
	userInfo.Fish += number
	err = sql.db.Insert("fishState", &userInfo)
	return
}

// 更新诅咒
func (sql *fishdb) updateCurseFor(uid int64, info string, number int) (err error) {
	if number < 1 {
		return
	}
	sql.Lock()
	defer sql.Unlock()
	userInfo := fishState{ID: uid}
	err = sql.db.Create("fishState", &userInfo)
	if err != nil {
		return err
	}
	changeCheck := false
	add := 0
	buffName := "宝藏诅咒"
	_ = sql.db.Find("fishState", &userInfo, "WHERE ID = ?", uid)
	if info == "fish" {
		userInfo.Bless += number
		for userInfo.Bless >= 75 {
			add++
			changeCheck = true
			buffName = "净化书"
			userInfo.Bless -= 75
		}
	} else {
		userInfo.Curse += number
		for userInfo.Curse >= 10 {
			add++
			changeCheck = true
			userInfo.Curse -= 10
		}
	}
	err = sql.db.Insert("fishState", &userInfo)
	if err != nil {
		return err
	}
	if changeCheck {
		table := strconv.FormatInt(uid, 10) + "Pack"
		thing := article{
			Duration: time.Now().Unix(),
			Name:     buffName,
			Type:     "treasure",
		}
		_ = sql.db.Find(table, &thing, "WHERE Name = ?", buffName)
		thing.Number += add
		return sql.db.Insert(table, &thing)
	}
	return
}

/*********************************************************/
/************************装备相关函数***********************/
/*********************************************************/

func (sql *fishdb) checkEquipFor(uid int64) (ok bool, err error) {
	sql.Lock()
	defer sql.Unlock()
	userInfo := fishState{ID: uid}
	err = sql.db.Create("fishState", &userInfo)
	if err != nil {
		return false, err
	}
	if !sql.db.CanFind("fishState", "WHERE ID = ?", uid) {
		return true, nil
	}
	err = sql.db.Find("fishState", &userInfo, "WHERE ID = ?", uid)
	if err != nil {
		return false, err
	}
	if userInfo.Equip > 3 {
		return false, nil
	}
	return true, nil
}

func (sql *fishdb) setEquipFor(uid int64) (err error) {
	sql.Lock()
	defer sql.Unlock()
	userInfo := fishState{ID: uid}
	err = sql.db.Create("fishState", &userInfo)
	if err != nil {
		return err
	}
	_ = sql.db.Find("fishState", &userInfo, "WHERE ID = ?", uid)
	userInfo.Equip++
	return sql.db.Insert("fishState", &userInfo)
}

// 获取装备信息
func (sql *fishdb) getUserEquip(uid int64) (userInfo equip, err error) {
	sql.Lock()
	defer sql.Unlock()

	// 尝试使用旧结构体读取数据
	type oldEquip struct {
		ID          int64
		Equip       string
		Durable     int
		Maintenance int
		Induce      int
		Favor       int
	}

	var oldTemp oldEquip
	err = sql.db.Create("equips", &oldTemp)
	if err != nil {
		return
	}
	if !sql.db.CanFind("equips", "WHERE ID = ?", uid) {
		return
	}

	// 尝试使用旧结构体读取数据
	err = sql.db.Find("equips", &oldTemp, "WHERE ID = ?", uid)
	if err == nil {
		// 成功读取旧结构体数据
		userInfo.ID = oldTemp.ID
		userInfo.Equip = oldTemp.Equip
		userInfo.Durable = oldTemp.Durable
		userInfo.Maintenance = oldTemp.Maintenance
		userInfo.Induce = oldTemp.Induce
		userInfo.Favor = oldTemp.Favor

		// 检查是否有附魔信息
		// 从背包中查找该装备的附魔信息
		packName := strconv.FormatInt(uid, 10) + "Pack"
		var packItem article
		var durabilityLevel, expRepairLevel int

		// 尝试从背包中找到对应的鱼竿
		if sql.db.CanFind(packName, "WHERE Name = ? AND Type = 'pole'", oldTemp.Equip) {
			var items []article
			err := sql.db.FindFor(packName, &packItem, "WHERE Name = ? AND Type = 'pole'", func() error {
				items = append(items, packItem)
				return nil
			}, oldTemp.Equip)

			if err == nil && len(items) > 0 {
				// 找到了背包中的鱼竿，尝试解析附魔信息
				for _, item := range items {
					poleInfo := strings.Split(item.Other, "/")
					if len(poleInfo) > 4 {
						durabilityLevel, _ = strconv.Atoi(poleInfo[4])
					}
					if len(poleInfo) > 5 {
						expRepairLevel, _ = strconv.Atoi(poleInfo[5])
					}
					// 找到一个有附魔的就跳出
					if durabilityLevel > 0 || expRepairLevel > 0 {
						break
					}
				}
			}
		}

		userInfo.Durability = durabilityLevel
		userInfo.ExpRepair = expRepairLevel

		// 升级数据库表结构（不使用异步，避免数据库锁定问题）
		// 创建一个新的临时结构体，包含所有字段
		type tempEquip struct {
			ID          int64
			Equip       string
			Durable     int
			Maintenance int
			Induce      int
			Favor       int
			Durability  int
			ExpRepair   int
		}

		// 将旧数据复制到新结构体中，包括附魔信息
		newTemp := tempEquip{
			ID:          oldTemp.ID,
			Equip:       oldTemp.Equip,
			Durable:     oldTemp.Durable,
			Maintenance: oldTemp.Maintenance,
			Induce:      oldTemp.Induce,
			Favor:       oldTemp.Favor,
			Durability:  durabilityLevel,
			ExpRepair:   expRepairLevel,
		}

		// 删除旧数据
		_ = sql.db.Del("equips", "WHERE ID = ?", uid)

		// 插入新数据
		_ = sql.db.Insert("equips", &newTemp)

		return
	}

	// 如果使用旧结构体读取失败，尝试使用新结构体读取数据
	type tempEquip struct {
		ID          int64
		Equip       string
		Durable     int
		Maintenance int
		Induce      int
		Favor       int
		Durability  int
		ExpRepair   int
	}

	var temp tempEquip
	err = sql.db.Create("equips", &temp)
	if err != nil {
		return
	}
	if !sql.db.CanFind("equips", "WHERE ID = ?", uid) {
		return
	}
	err = sql.db.Find("equips", &temp, "WHERE ID = ?", uid)
	if err != nil {
		return
	}

	// 将临时结构体的值复制到返回的结构体中
	userInfo.ID = temp.ID
	userInfo.Equip = temp.Equip
	userInfo.Durable = temp.Durable
	userInfo.Maintenance = temp.Maintenance
	userInfo.Induce = temp.Induce
	userInfo.Favor = temp.Favor
	userInfo.Durability = temp.Durability
	userInfo.ExpRepair = temp.ExpRepair

	// 添加日志，记录从数据库读取的附魔等级
	logrus.Infof("从数据库读取的附魔等级 - 耐久附魔: %d, 经验修补: %d", temp.Durability, temp.ExpRepair)

	// 始终尝试从背包中查找对应鱼竿的附魔信息，确保数据一致性
	if temp.Equip != "" && temp.Equip != "美西螈" {
		packName := strconv.FormatInt(uid, 10) + "Pack"
		var packItem article

		// 尝试从背包中找到对应的鱼竿
		if sql.db.CanFind(packName, "WHERE Name = ? AND Type = 'pole'", temp.Equip) {
			var items []article
			err := sql.db.FindFor(packName, &packItem, "WHERE Name = ? AND Type = 'pole'", func() error {
				items = append(items, packItem)
				return nil
			}, temp.Equip)

			if err == nil && len(items) > 0 {
				// 找到了背包中的鱼竿，尝试解析附魔信息
				maxDurabilityLevel := 0
				maxExpRepairLevel := 0

				for _, item := range items {
					poleInfo := strings.Split(item.Other, "/")
					logrus.Infof("从背包解析鱼竿属性，原始字符串: %s", item.Other)

					if len(poleInfo) > 4 {
						durabilityLevel, _ := strconv.Atoi(poleInfo[4])
						logrus.Infof("从背包解析的耐久附魔等级: %d, 原始值: %s", durabilityLevel, poleInfo[4])
						if durabilityLevel > maxDurabilityLevel {
							maxDurabilityLevel = durabilityLevel
						}
					}

					if len(poleInfo) > 5 {
						expRepairLevel, _ := strconv.Atoi(poleInfo[5])
						logrus.Infof("从背包解析的经验修补附魔等级: %d, 原始值: %s", expRepairLevel, poleInfo[5])
						if expRepairLevel > maxExpRepairLevel {
							maxExpRepairLevel = expRepairLevel
						}
					}
				}

				// 使用背包中找到的最高附魔等级
				if maxDurabilityLevel > 0 {
					userInfo.Durability = maxDurabilityLevel
					logrus.Infof("从背包更新的耐久附魔等级: %d", maxDurabilityLevel)
				}

				if maxExpRepairLevel > 0 {
					userInfo.ExpRepair = maxExpRepairLevel
					logrus.Infof("从背包更新的经验修补附魔等级: %d", maxExpRepairLevel)
				}

				// 如果背包中的附魔等级与数据库中的不一致，更新数据库
				if userInfo.Durability != temp.Durability || userInfo.ExpRepair != temp.ExpRepair {
					logrus.Infof("数据库中的附魔等级与背包中的不一致，更新数据库 - 数据库: 耐久附魔 %d, 经验修补 %d, 背包: 耐久附魔 %d, 经验修补 %d",
						temp.Durability, temp.ExpRepair, userInfo.Durability, userInfo.ExpRepair)

					// 更新数据库中的附魔等级
					temp.Durability = userInfo.Durability
					temp.ExpRepair = userInfo.ExpRepair
					_ = sql.db.Insert("equips", &temp)
				}
			}
		}
	}

	return
}

// 更新装备信息
func (sql *fishdb) updateUserEquip(userInfo equip) (err error) {
	sql.Lock()
	defer sql.Unlock()

	// 添加日志，记录要保存的附魔等级
	logrus.Infof("保存到数据库的附魔等级 - 耐久附魔: %d, 经验修补: %d", userInfo.Durability, userInfo.ExpRepair)

	// 尝试使用旧结构体检查表结构
	type oldEquip struct {
		ID          int64
		Equip       string
		Durable     int
		Maintenance int
		Induce      int
		Favor       int
	}

	var oldTemp oldEquip
	err = sql.db.Create("equips", &oldTemp)
	if err != nil {
		return
	}

	// 尝试使用旧结构体读取数据
	err = sql.db.Find("equips", &oldTemp, "WHERE ID = ?", userInfo.ID)
	if err == nil {
		// 成功读取旧结构体数据，说明表结构是旧的
		// 需要升级表结构

		// 创建一个新的临时结构体，包含所有字段
		type tempEquip struct {
			ID          int64
			Equip       string
			Durable     int
			Maintenance int
			Induce      int
			Favor       int
			Durability  int
			ExpRepair   int
		}

		// 将userInfo的值复制到新结构体中
		newTemp := tempEquip{
			ID:          userInfo.ID,
			Equip:       userInfo.Equip,
			Durable:     userInfo.Durable,
			Maintenance: userInfo.Maintenance,
			Induce:      userInfo.Induce,
			Favor:       userInfo.Favor,
			Durability:  userInfo.Durability,
			ExpRepair:   userInfo.ExpRepair,
		}

		// 删除旧数据
		_ = sql.db.Del("equips", "WHERE ID = ?", userInfo.ID)

		// 创建新表
		_ = sql.db.Create("equips", &newTemp)

		if userInfo.Durable == 0 {
			return sql.db.Del("equips", "WHERE ID = ?", userInfo.ID)
		}

		// 插入新数据
		return sql.db.Insert("equips", &newTemp)
	}

	// 如果使用旧结构体读取失败，尝试使用新结构体
	type tempEquip struct {
		ID          int64
		Equip       string
		Durable     int
		Maintenance int
		Induce      int
		Favor       int
		Durability  int
		ExpRepair   int
	}

	// 将userInfo的值复制到临时结构体中
	temp := tempEquip{
		ID:          userInfo.ID,
		Equip:       userInfo.Equip,
		Durable:     userInfo.Durable,
		Maintenance: userInfo.Maintenance,
		Induce:      userInfo.Induce,
		Favor:       userInfo.Favor,
		Durability:  userInfo.Durability,
		ExpRepair:   userInfo.ExpRepair,
	}

	err = sql.db.Create("equips", &temp)
	if err != nil {
		return
	}
	if userInfo.Durable == 0 {
		return sql.db.Del("equips", "WHERE ID = ?", userInfo.ID)
	}

	// 插入数据
	err = sql.db.Insert("equips", &temp)

	// 如果成功插入，再次检查附魔等级是否正确保存
	if err == nil {
		var checkTemp tempEquip
		checkErr := sql.db.Find("equips", &checkTemp, "WHERE ID = ?", userInfo.ID)
		if checkErr == nil {
			logrus.Infof("检查保存后的附魔等级 - 耐久附魔: %d, 经验修补: %d", checkTemp.Durability, checkTemp.ExpRepair)

			// 如果附魔等级不一致，再次尝试保存
			if checkTemp.Durability != userInfo.Durability || checkTemp.ExpRepair != userInfo.ExpRepair {
				logrus.Infof("附魔等级不一致，重新保存")

				// 删除旧数据
				_ = sql.db.Del("equips", "WHERE ID = ?", userInfo.ID)

				// 重新插入数据
				return sql.db.Insert("equips", &temp)
			}
		}
	}

	return err
}

func (sql *fishdb) pickFishFor(uid int64, number int) (fishNames map[string]int, err error) {
	fishNames = make(map[string]int, 6)
	name := strconv.FormatInt(uid, 10) + "Pack"
	sql.Lock()
	defer sql.Unlock()
	userInfo := article{}
	err = sql.db.Create(name, &userInfo)
	if err != nil {
		return
	}
	count, err := sql.db.Count(name)
	if err != nil {
		return
	}
	if count == 0 {
		return
	}
	if !sql.db.CanFind(name, "WHERE Type = 'fish'") {
		return
	}
	fishInfo := article{}
	k := 0
	for i := number; i > 0 && k < len(fishList); {
		_ = sql.db.Find(name, &fishInfo, "WHERE Name = ?", fishList[k])
		if fishInfo.Number <= 0 {
			k++
			continue
		}
		if fishInfo.Number < i {
			k++
			i -= fishInfo.Number
			fishNames[fishInfo.Name] += fishInfo.Number
			fishInfo.Number = 0
		} else {
			fishNames[fishInfo.Name] += i
			fishInfo.Number -= i
			i = 0
		}
		if fishInfo.Number <= 0 {
			err = sql.db.Del(name, "WHERE Duration = ?", fishInfo.Duration)
		} else {
			err = sql.db.Insert(name, &fishInfo)
		}
		if err != nil {
			return
		}
	}
	return
}

/*********************************************************/
/************************背包相关函数***********************/
/*********************************************************/

// 获取用户背包信息
func (sql *fishdb) getUserPack(uid int64) (thingInfos []article, err error) {
	sql.Lock()
	defer sql.Unlock()
	userInfo := article{}
	err = sql.db.Create(strconv.FormatInt(uid, 10)+"Pack", &userInfo)
	if err != nil {
		return
	}
	count, err := sql.db.Count(strconv.FormatInt(uid, 10) + "Pack")
	if err != nil {
		return
	}
	if count == 0 {
		return
	}
	err = sql.db.FindFor(strconv.FormatInt(uid, 10)+"Pack", &userInfo, "ORDER by Type, Name, Other ASC", func() error {
		thingInfos = append(thingInfos, userInfo)
		return nil
	})
	return
}

// 获取用户物品信息
func (sql *fishdb) getUserThingInfo(uid int64, thing string) (thingInfos []article, err error) {
	name := strconv.FormatInt(uid, 10) + "Pack"
	sql.Lock()
	defer sql.Unlock()
	userInfo := article{}
	err = sql.db.Create(name, &userInfo)
	if err != nil {
		return
	}
	count, err := sql.db.Count(name)
	if err != nil {
		return
	}
	if count == 0 {
		return
	}
	if !sql.db.CanFind(name, "WHERE Name = ?", thing) {
		return
	}
	err = sql.db.FindFor(name, &userInfo, "WHERE Name = ?", func() error {
		thingInfos = append(thingInfos, userInfo)
		return nil
	}, thing)
	return
}

// 更新用户物品信息
func (sql *fishdb) updateUserThingInfo(uid int64, userInfo article) (err error) {
	name := strconv.FormatInt(uid, 10) + "Pack"
	sql.Lock()
	defer sql.Unlock()
	err = sql.db.Create(name, &userInfo)
	if err != nil {
		return
	}
	if userInfo.Number == 0 {
		return sql.db.Del(name, "WHERE Duration = ?", userInfo.Duration)
	}
	return sql.db.Insert(name, &userInfo)
}

// 获取某关键字的数量
func (sql *fishdb) getNumberFor(uid int64, thing string) (number int, err error) {
	name := strconv.FormatInt(uid, 10) + "Pack"
	sql.Lock()
	defer sql.Unlock()
	userInfo := article{}
	err = sql.db.Create(name, &userInfo)
	if err != nil {
		return
	}
	count, err := sql.db.Count(name)
	if err != nil {
		return
	}
	if count == 0 {
		return
	}
	if !sql.db.CanFind(name, "WHERE Name glob ?", "*"+thing+"*") {
		return
	}
	info := article{}
	err = sql.db.FindFor(name, &info, "WHERE Name glob ?", func() error {
		number += info.Number
		return nil
	}, "*"+thing+"*")
	return
}

// 获取用户的某类物品信息
func (sql *fishdb) getUserTypeInfo(uid int64, thingType string) (thingInfos []article, err error) {
	name := strconv.FormatInt(uid, 10) + "Pack"
	sql.Lock()
	defer sql.Unlock()
	userInfo := article{}
	err = sql.db.Create(name, &userInfo)
	if err != nil {
		return
	}
	if !sql.db.CanFind(name, "WHERE Type = ?", thingType) {
		return
	}
	err = sql.db.FindFor(name, &userInfo, "WHERE Type = ?", func() error {
		thingInfos = append(thingInfos, userInfo)
		return nil
	}, thingType)
	return
}

/*********************************************************/
/************************商店相关函数***********************/
/*********************************************************/

// 刷新商店信息
func (sql *fishdb) refreshStroeInfo() (ok bool, err error) {
	sql.Lock()
	defer sql.Unlock()
	err = sql.db.Create("stroeDiscount", &storeDiscount{})
	if err != nil {
		return false, err
	}
	err = sql.db.Create("store", &store{})
	if err != nil {
		return false, err
	}
	lastTime := storeDiscount{}
	_ = sql.db.Find("stroeDiscount", &lastTime, "WHERE Name = 'lastTime'")
	refresh := false
	timeNow := time.Now().Day()
	if timeNow != lastTime.Discount {
		lastTime = storeDiscount{
			Name:     "lastTime",
			Discount: timeNow,
		}
		err = sql.db.Insert("stroeDiscount", &lastTime)
		if err != nil {
			return false, err
		}
		refresh = true
	}
	for _, name := range thingList {
		thing := storeDiscount{}
		switch refresh {
		case true:
			thingDiscount := 50 + rand.Intn(150)
			thing = storeDiscount{
				Name:     name,
				Discount: thingDiscount,
			}
			thingInfo := store{}
			_ = sql.db.Find("store", &thingInfo, "WHERE Name = ?", name)
			if thingInfo.Number > 150 {
				// 控制价格浮动区间： -10%到10%
				thing.Discount = 90 + rand.Intn(20)
			}
			err = sql.db.Insert("stroeDiscount", &thing)
			if err != nil {
				return
			}
		default:
			_ = sql.db.Find("stroeDiscount", &thing, "WHERE Name = ?", name)
		}
		if thing.Discount != 0 {
			discountList[name] = thing.Discount
		} else {
			discountList[name] = 100
		}
	}
	thing := store{}
	var oldThing []store
	_ = sql.db.FindFor("stroeDiscount", &thing, "WHERE type = 'pole'", func() error {
		if time.Since(time.Unix(thing.Duration, 0)) > 24 {
			oldThing = append(oldThing, thing)
		}
		return nil
	})
	for _, info := range oldThing {
		_ = sql.db.Del("stroeDiscount", "WHERE Duration = ?", info.Duration)
	}
	if refresh {
		// 每天调控1种鱼
		fish := fishList[rand.Intn(len(fishList))]
		thingInfo := store{
			Duration: time.Now().Unix(),
			Name:     fish,
			Type:     "fish",
			Price:    priceList[fish] * discountList[fish] / 100,
		}
		_ = sql.db.Find("store", &thingInfo, "WHERE Name = ?", fish)
		thingInfo.Number += 100 - discountList[fish]
		if thingInfo.Number < 1 {
			thingInfo.Number = 100
		}
		_ = sql.db.Insert("store", &thingInfo)
		// 每天上架1木竿
		thingInfo = store{
			Duration: time.Now().Unix(),
			Name:     "初始木竿",
			Type:     "pole",
			Price:    priceList["木竿"] + priceList["木竿"]*discountList["木竿"]/100,
			Other:    "30/0/0/0",
		}
		_ = sql.db.Find("store", &thingInfo, "WHERE Name = '初始木竿'")
		thingInfo.Number++
		if thingInfo.Number > 5 {
			thingInfo.Number = 1
		}
		_ = sql.db.Insert("store", &thingInfo)
	}
	return true, nil
}

// 获取商店信息
func (sql *fishdb) getStoreInfo() (thingInfos []store, err error) {
	sql.Lock()
	defer sql.Unlock()
	thingInfo := store{}
	err = sql.db.Create("store", &thingInfo)
	if err != nil {
		return
	}
	count, err := sql.db.Count("store")
	if err != nil {
		return
	}
	if count == 0 {
		return
	}
	err = sql.db.FindFor("store", &thingInfo, "ORDER by Type, Name, Price ASC", func() error {
		thingInfos = append(thingInfos, thingInfo)
		return nil
	})
	return
}

// 获取商店物品信息
func (sql *fishdb) getStoreThingInfo(thing string) (thingInfos []store, err error) {
	sql.Lock()
	defer sql.Unlock()
	thingInfo := store{}
	err = sql.db.Create("store", &thingInfo)
	if err != nil {
		return
	}
	count, err := sql.db.Count("store")
	if err != nil {
		return
	}
	if count == 0 {
		return
	}
	if !sql.db.CanFind("store", "WHERE Name = ?", thing) {
		return
	}
	err = sql.db.FindFor("store", &thingInfo, "WHERE Name = ?", func() error {
		thingInfos = append(thingInfos, thingInfo)
		return nil
	}, thing)
	return
}

// 获取商店物品信息
func (sql *fishdb) checkStoreFor(thing store, number int) (ok bool, err error) {
	sql.Lock()
	defer sql.Unlock()
	err = sql.db.Create("store", &thing)
	if err != nil {
		return
	}
	count, err := sql.db.Count("store")
	if err != nil {
		return
	}
	if count == 0 {
		return false, nil
	}
	if !sql.db.CanFind("store", "WHERE Duration = ?", thing.Duration) {
		return false, nil
	}
	err = sql.db.Find("store", &thing, "WHERE Duration = ?", thing.Duration)
	if err != nil {
		return
	}
	if thing.Number < number {
		return false, nil
	}
	return true, nil
}

// 更新商店信息
func (sql *fishdb) updateStoreInfo(thingInfo store) (err error) {
	sql.Lock()
	defer sql.Unlock()
	err = sql.db.Create("store", &thingInfo)
	if err != nil {
		return
	}
	if thingInfo.Number == 0 {
		return sql.db.Del("store", "WHERE Duration = ?", thingInfo.Duration)
	}
	return sql.db.Insert("store", &thingInfo)
}

// 更新购买次数
func (sql *fishdb) updateBuyTimeFor(uid int64, add int) (err error) {
	sql.Lock()
	defer sql.Unlock()
	userInfo := buffInfo{ID: uid}
	err = sql.db.Create("buff", &userInfo)
	if err != nil {
		return err
	}
	_ = sql.db.Find("buff", &userInfo, "WHERE ID = ?", uid)
	userInfo.BuyTimes += add
	if userInfo.BuyTimes > 20 {
		userInfo.BuyTimes -= 20
		userInfo.Coupon = 3
	}
	return sql.db.Insert("buff", &userInfo)
}

// 使用优惠卷
func (sql *fishdb) useCouponAt(uid int64, times int) (int, error) {
	useTimes := -1
	sql.Lock()
	defer sql.Unlock()
	userInfo := buffInfo{ID: uid}
	err := sql.db.Create("buff", &userInfo)
	if err != nil {
		return useTimes, err
	}
	_ = sql.db.Find("buff", &userInfo, "WHERE ID = ?", uid)
	if userInfo.Coupon > 0 {
		useTimes = math.Min(userInfo.Coupon, times)
		userInfo.Coupon -= useTimes
	}
	return useTimes, sql.db.Insert("buff", &userInfo)
}

// 买卖上限检测
func (sql *fishdb) checkCanSalesFor(uid int64, saleName string, salesNum int) (int, error) {
	sql.Lock()
	defer sql.Unlock()
	userInfo := buffInfo{ID: uid}
	err := sql.db.Create("buff", &userInfo)
	if err != nil {
		return salesNum, err
	}
	_ = sql.db.Find("buff", &userInfo, "WHERE ID = ?", uid)
	if time.Now().Day() != time.Unix(userInfo.Duration, 0).Day() {
		userInfo.Duration = time.Now().Unix()
		userInfo.SalesPole = 0
		userInfo.BuyTing = 0
		err := sql.db.Insert("buff", &userInfo)
		if err != nil {
			return salesNum, err
		}
	}
	if strings.Contains(saleName, "竿") {
		if userInfo.SalesPole >= 10 {
			salesNum = -1
		}
	} else if !checkIsWaste(saleName) {
		maxSales := 30 - userInfo.BuyTing
		if maxSales < 0 {
			salesNum = 0
		}
		if salesNum > maxSales {
			salesNum = maxSales
		}
	}

	return salesNum, err
}

// 更新买卖鱼上限，假定sales变量已经在 checkCanSalesFor 进行了防护
func (sql *fishdb) updateCanSalesFor(uid int64, saleName string, sales int) error {
	sql.Lock()
	defer sql.Unlock()
	userInfo := buffInfo{ID: uid}
	err := sql.db.Create("buff", &userInfo)
	if err != nil {
		return err
	}
	_ = sql.db.Find("buff", &userInfo, "WHERE ID = ?", uid)
	if strings.Contains(saleName, "竿") {
		userInfo.SalesPole++
	} else if !checkIsWaste(saleName) {
		userInfo.BuyTing += sales
	}
	return sql.db.Insert("buff", &userInfo)
}

// 检测物品是否是垃圾
func checkIsWaste(thing string) bool {
	for _, v := range wasteList {
		if v == thing {
			return true
		}
	}
	return false
}
