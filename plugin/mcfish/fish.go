// Package mcfish 钓鱼模拟器
package mcfish

import (
	"math/rand"
	"strconv"
	"strings"
	"time"

	"github.com/FloatTech/AnimeAPI/wallet"
	"github.com/FloatTech/zbputils/ctxext"
	"github.com/sirupsen/logrus"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

// 处理钓鱼逻辑，减少锁的使用
func processFishing(uid int64, fishNumber int, equipInfo equip) (residue int, newEquipInfo equip, errMsg, msg string) {
	newEquipInfo = equipInfo

	// 更新钓鱼次数
	residue, err := dbdata.updateFishInfo(uid, fishNumber)
	if err != nil {
		errMsg = "[ERROR at fish.go.1]:" + err.Error()
		return
	}

	if residue == 0 {
		return
	}

	if equipInfo.Equip != "美西螈" {
		// 耐久附魔逻辑：根据等级计算是否消耗耐久
		durabilityConsumption := residue

		// 打印调试信息
		logrus.Infof("耐久附魔等级: %d", equipInfo.Durability)

		for i := 0; i < residue; i++ {
			// 计算概率：(60 + 40/(等级+1))%
			probability := 60 + 40/(equipInfo.Durability+1)
			roll := rand.Intn(100)
			logrus.Infof("耐久检定: 需要 < %d, 实际 = %d", probability, roll)

			if roll < probability {
				durabilityConsumption--
				logrus.Infof("耐久检定成功，不消耗耐久")
			} else {
				logrus.Infof("耐久检定失败，消耗耐久")
			}
		}

		// 经验修补附魔逻辑：消耗金钱修复耐久
		logrus.Infof("经验修补附魔等级: %d", equipInfo.ExpRepair)

		if equipInfo.ExpRepair > 0 && equipInfo.Durable < durationList[equipInfo.Equip] {
			// 计算需要修复的耐久值
			repairNeeded := durationList[equipInfo.Equip] - equipInfo.Durable
			logrus.Infof("需要修复的耐久值: %d", repairNeeded)

			// 获取用户钱包余额
			money := wallet.GetWalletOf(uid)
			logrus.Infof("用户钱包余额: %d", money)

			if money >= 2 { // 至少有2金钱才能修复
				// 计算实际可以修复的耐久值
				actualRepair := money / 2
				if actualRepair > repairNeeded {
					actualRepair = repairNeeded
				}
				logrus.Infof("实际修复的耐久值: %d", actualRepair)

				// 扣除金钱
				err = wallet.InsertWalletOf(uid, -actualRepair*2)
				if err != nil {
					errMsg = "[ERROR at fish.go.5.2]:" + err.Error()
					return
				}
				// 增加耐久
				newEquipInfo.Durable += actualRepair
				msg += "(经验修补：消耗" + strconv.Itoa(actualRepair*2) + wallet.GetWalletName() + "修复了" + strconv.Itoa(actualRepair) + "点耐久)"
			} else {
				logrus.Infof("钱包余额不足，无法修复耐久")
			}
		} else {
			if equipInfo.ExpRepair <= 0 {
				logrus.Infof("经验修补附魔等级为0，无法修复耐久")
			}
			if equipInfo.Durable >= durationList[equipInfo.Equip] {
				logrus.Infof("耐久已满，无需修复")
			}
		}

		// 消耗耐久
		newEquipInfo.Durable -= durabilityConsumption
		err = dbdata.updateUserEquip(newEquipInfo)
		if err != nil {
			errMsg = "[ERROR at fish.go.5]:" + err.Error()
			return
		}

		if newEquipInfo.Durable < 10 && newEquipInfo.Durable > 0 {
			msg += "(你的鱼竿耐久仅剩" + strconv.Itoa(newEquipInfo.Durable) + ")"
		} else if newEquipInfo.Durable <= 0 {
			msg += "(你的鱼竿已销毁)"
		}
	}

	return
}

func init() {
	engine.OnRegex(`^进行(([1-5]\d|[1-9])次)?钓鱼$`, getdb).SetBlock(true).Limit(ctxext.LimitByUser).Handle(func(ctx *zero.Ctx) {
		uid := ctx.Event.UserID
		numberOfPole, err := dbdata.getNumberFor(uid, "竿")
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at store.go.9.3]:", err))
			return
		}
		if numberOfPole > 100 {
			ctx.SendChain(message.Text("你有", numberOfPole, "支鱼竿,大于100支的玩家不允许钓鱼"))
			return
		}
		fishNumber := 1
		info := ctx.State["regex_matched"].([]string)[2]
		if info != "" {
			number, err := strconv.Atoi(info)
			if err != nil || number > FishLimit {
				ctx.SendChain(message.Text("请输入正确的次数"))
				return
			}
			fishNumber = number
		}
		equipInfo, err := dbdata.getUserEquip(uid)
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at fish.go.2]:", err))
			return
		}
		if equipInfo == (equip{}) {
			ok, err := dbdata.checkEquipFor(uid)
			if err != nil {
				ctx.SendChain(message.Text("[ERROR at fish.go.2.1]:", err))
				return
			}
			if !ok {
				ctx.SendChain(message.At(uid), message.Text("请装备鱼竿后钓鱼", err))
				return
			}
			ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("你尚未装备鱼竿,是否花费100购买鱼竿?\n回答\"是\"或\"否\""))
			// 等待用户下一步选择
			recv, cancel := zero.NewFutureEvent("message", 999, false, zero.RegexRule(`^(是|否)$`), zero.CheckUser(ctx.Event.UserID)).Repeat()
			defer cancel()
			buy := false
			for {
				select {
				case <-time.After(time.Second * 120):
					ctx.Send(message.ReplyWithMessage(ctx.Event.MessageID, message.Text("等待超时,取消购买")))
					return
				case e := <-recv:
					nextcmd := e.Event.Message.String()
					if nextcmd == "否" {
						ctx.Send(message.ReplyWithMessage(ctx.Event.MessageID, message.Text("已取消购买")))
						return
					}
					money := wallet.GetWalletOf(uid)
					if money < 100 {
						ctx.SendChain(message.Text("你钱包当前只有", money, wallet.GetWalletName(), ",无法完成支付"))
						return
					}
					err = wallet.InsertWalletOf(uid, -100)
					if err != nil {
						ctx.SendChain(message.Text("[ERROR at fish.go.3]:", err))
						return
					}
					equipInfo = equip{
						ID:      uid,
						Equip:   "木竿",
						Durable: 30,
					}
					err = dbdata.updateUserEquip(equipInfo)
					if err != nil {
						ctx.SendChain(message.Text("[ERROR at fish.go.4]:", err))
						return
					}
					err = dbdata.setEquipFor(uid)
					if err != nil {
						ctx.SendChain(message.Text("[ERROR at fish.go.4]:", err))
						return
					}
					buy = true
				}
				if buy {
					break
				}
			}
		}
		if equipInfo.Durable < fishNumber {
			fishNumber = equipInfo.Durable
		}
		// 使用一个函数来处理钓鱼逻辑，减少锁的使用
		residue, newEquipInfo, errMsg, newMsg := processFishing(uid, fishNumber, equipInfo)
		if errMsg != "" {
			ctx.SendChain(message.Text(errMsg))
			return
		}
		if residue == 0 {
			ctx.SendChain(message.Text("今天你已经进行", FishLimit, "次钓鱼了.\n游戏虽好,但请不要沉迷。"))
			return
		}
		fishNumber = residue
		msg := newMsg
		equipInfo = newEquipInfo

		if equipInfo.Equip == "三叉戟" {
			fishNumber *= 3
		}

		if equipInfo.Equip == "美西螈" {
			fishNames, err := dbdata.pickFishFor(uid, fishNumber*3)
			if err != nil {
				ctx.SendChain(message.Text("[ERROR at fish.go.5.1]:", err))
				return
			}
			if len(fishNames) == 0 {
				equipInfo.Durable = 0
				err = dbdata.updateUserEquip(equipInfo)
				if err != nil {
					ctx.SendChain(message.Text("[ERROR at fish.go.5]:", err))
				}
				ctx.SendChain(message.Text("美西螈因为没吃到鱼,钓鱼时一直没回来,你失去了美西螈"))
				return
			}
			msg = "(美西螈掉落翻5倍，吃3倍鱼：\n吃掉了："
			fishNumber = 0
			for name, number := range fishNames {
				fishNumber += number
				msg += strconv.Itoa(number) + name + " "
			}
			msg += ")"
			fishNumber /= 3
		}
		waitTime := 120 / (equipInfo.Induce + 1)
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text("你开始去钓鱼了,请耐心等待鱼上钩(预计要", time.Second*time.Duration(waitTime), ")"))
		timer := time.NewTimer(time.Second * time.Duration(rand.Intn(waitTime)+1))
		for {
			<-timer.C
			timer.Stop()
			break
		}
		// 钓到鱼的范围
		number, err := dbdata.getNumberFor(uid, "鱼")
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at fish.go.5.1]:", err))
			return
		}
		number2, err := dbdata.getNumberFor(uid, "海豚")
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at fish.go.5.1]:", err))
			return
		}
		if number > 100 || equipInfo.Equip == "美西螈" { // 放大概率
			probabilities["treasure"] = probabilityLimit{
				Min: 0,
				Max: 2,
			}
			probabilities["pole"] = probabilityLimit{
				Min: 2,
				Max: 10,
			}
			probabilities["fish"] = probabilityLimit{
				Min: 10,
				Max: 45,
			}
			probabilities["waste"] = probabilityLimit{
				Min: 45,
				Max: 90,
			}
		}
		if number2 != 0 {
			info := probabilities["waste"]
			info.Max = 100
			probabilities["waste"] = info
		}
		for name, info := range probabilities {
			switch name {
			case "treasure":
				info.Max += equipInfo.Favor
				probabilities[name] = info
			case "pole":
				info.Min += equipInfo.Favor
				info.Max += equipInfo.Favor * 2
				probabilities[name] = info
			case "fish":
				info.Min += equipInfo.Favor * 2
				info.Max += equipInfo.Favor * 3
				probabilities[name] = info
			case "waste":
				info.Min += equipInfo.Favor * 3
				probabilities[name] = info
			}
		}
		// 钓鱼结算
		picName := ""
		thingNameList := make(map[string]int)
		for i := fishNumber; i > 0; i-- {
			thingName := ""
			typeOfThing := ""
			number := 1
			dice := rand.Intn(100)
			switch {
			case dice >= probabilities["waste"].Min && dice < probabilities["waste"].Max: // 垃圾
				typeOfThing = "waste"
				thingName = wasteList[rand.Intn(len(wasteList))]
				picName = thingName
			case dice >= probabilities["treasure"].Min && dice < probabilities["treasure"].Max: // 宝藏
				dice = rand.Intn(100)
				switch {
				case dice >= probabilities["美西螈"].Min && dice < probabilities["美西螈"].Max:
					typeOfThing = "pole"
					picName = "美西螈"
					thingName = "美西螈"
				case dice >= probabilities["唱片"].Min && dice < probabilities["唱片"].Max:
					typeOfThing = "article"
					picName = "唱片"
					thingName = "唱片"
				case dice >= probabilities["海之眷顾"].Min && dice < probabilities["海之眷顾"].Max:
					typeOfThing = "article"
					picName = "book"
					thingName = "海之眷顾"
				case dice >= probabilities["净化书"].Min && dice < probabilities["净化书"].Max:
					typeOfThing = "article"
					picName = "book"
					thingName = "净化书"
				case dice >= probabilities["宝藏诅咒"].Min && dice < probabilities["宝藏诅咒"].Max:
					typeOfThing = "article"
					picName = "book"
					thingName = "宝藏诅咒"
				case dice >= probabilities["海豚"].Min && dice < probabilities["海豚"].Max:
					typeOfThing = "fish"
					picName = "海豚"
					thingName = "海豚"
				case dice >= probabilities["耐久"].Min && dice < probabilities["耐久"].Max:
					typeOfThing = "article"
					picName = "book"
					thingName = "耐久"
				case dice >= probabilities["经验修补"].Min && dice < probabilities["经验修补"].Max:
					typeOfThing = "article"
					picName = "book"
					thingName = "经验修补"
				default:
					typeOfThing = "article"
					picName = "book"
					thingName = "诱钓"
				}
			case dice >= probabilities["pole"].Min && dice < probabilities["pole"].Max: // 宝藏
				typeOfThing = "pole"
				dice := rand.Intn(100)
				switch {
				case dice >= probabilities["铁竿"].Min && dice < probabilities["铁竿"].Max:
					thingName = "铁竿"
				case dice >= probabilities["金竿"].Min && dice < probabilities["金竿"].Max:
					thingName = "金竿"
				case dice >= probabilities["钻石竿"].Min && dice < probabilities["钻石竿"].Max:
					thingName = "钻石竿"
				case dice >= probabilities["下界合金竿"].Min && dice < probabilities["下界合金竿"].Max:
					thingName = "下界合金竿"
				default:
					thingName = "木竿"
				}
				picName = thingName
			case dice >= probabilities["fish"].Min && dice < probabilities["fish"].Max:
				typeOfThing = "fish"
				dice = rand.Intn(100)
				switch {
				case dice >= probabilities["墨鱼"].Min && dice < probabilities["墨鱼"].Max:
					thingName = "墨鱼"
				case dice >= probabilities["鳕鱼"].Min && dice < probabilities["鳕鱼"].Max:
					thingName = "鳕鱼"
				case dice >= probabilities["鲑鱼"].Min && dice < probabilities["鲑鱼"].Max:
					thingName = "鲑鱼"
				case dice >= probabilities["热带鱼"].Min && dice < probabilities["热带鱼"].Max:
					thingName = "热带鱼"
				case dice >= probabilities["河豚"].Min && dice < probabilities["河豚"].Max:
					thingName = "河豚"
				default:
					thingName = "鹦鹉螺"
				}
				picName = thingName
			default:
				thingNameList["赛博空气"]++
			}
			if thingName != "" {
				newThing := article{}
				if strings.Contains(thingName, "竿") {
					// 随机生成鱼竿属性，包括耐久附魔和经验修补附魔
					durabilityLevel := rand.Intn(3) // 0-2
					expRepairLevel := rand.Intn(2)  // 0-1

					// 打印调试信息
					logrus.Infof("生成鱼竿: %s, 耐久附魔等级: %d, 经验修补附魔等级: %d", thingName, durabilityLevel, expRepairLevel)

					info := strconv.Itoa(rand.Intn(durationList[thingName])+1) +
						"/" + strconv.Itoa(rand.Intn(10)) + "/" +
						strconv.Itoa(rand.Intn(3)) + "/" + strconv.Itoa(rand.Intn(2)) + "/" +
						strconv.Itoa(durabilityLevel) + "/" + strconv.Itoa(expRepairLevel)
					newThing = article{
						Duration: time.Now().Unix()*100 + int64(i),
						Type:     typeOfThing,
						Name:     thingName,
						Number:   number,
						Other:    info,
					}
				} else {
					thingInfo, err := dbdata.getUserThingInfo(uid, thingName)
					if err != nil {
						ctx.SendChain(message.Text("[ERROR at fish.go.6]:", err))
						return
					}
					if len(thingInfo) == 0 {
						newThing = article{
							Duration: time.Now().Unix()*100 + int64(i),
							Type:     typeOfThing,
							Name:     thingName,
						}
					} else {
						newThing = thingInfo[0]
					}
					if equipInfo.Equip == "美西螈" && thingName != "美西螈" {
						number += 4
					}
					newThing.Number += number
				}
				err = dbdata.updateUserThingInfo(uid, newThing)
				if err != nil {
					ctx.SendChain(message.Text("[ERROR at fish.go.7]:", err))
					return
				}
				thingNameList[thingName] += number
			}
		}
		err = dbdata.updateCurseFor(uid, "fish", fishNumber)
		if err != nil {
			logrus.Warnln(err)
		}
		if len(thingNameList) == 1 {
			thingName := ""
			numberOfFish := 0
			for name, number := range thingNameList {
				thingName = name
				numberOfFish = number
			}
			if picName != "" {
				pic, err := engine.GetLazyData(picName+".png", false)
				if err != nil {
					logrus.Warnln("[mcfish]error:", err)
					ctx.SendChain(message.At(uid), message.Text("恭喜你钓到了", numberOfFish, thingName, "\n", msg))
					return
				}
				ctx.SendChain(message.At(uid), message.Text("恭喜你钓到了", numberOfFish, thingName, "\n", msg), message.ImageBytes(pic))
				return
			}
			ctx.SendChain(message.At(uid), message.Text("恭喜你钓到了", numberOfFish, thingName, "\n", msg))
			return
		}
		msgInfo := make(message.Message, 0, 3+len(thingNameList))
		msgInfo = append(msgInfo, message.Reply(ctx.Event.MessageID), message.Text("你进行了", fishNumber, "次钓鱼,结果如下:\n"))
		for name, number := range thingNameList {
			msgInfo = append(msgInfo, message.Text(name, " : ", number, "\n"))
		}
		msgInfo = append(msgInfo, message.Text(msg))
		ctx.Send(msgInfo)
	})
}
