// Package niuniu 牛牛大作战
package niuniu

import (
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/FloatTech/AnimeAPI/niu"
	"github.com/FloatTech/AnimeAPI/wallet"
	sql "github.com/FloatTech/sqlite"
	ctrl "github.com/FloatTech/zbpctrl"
	"github.com/FloatTech/zbputils/control"
	"github.com/FloatTech/zbputils/ctxext"
	"github.com/RomiChan/syncx"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/extension/rate"
	"github.com/wdvxdr1123/ZeroBot/message"
)

type lastLength struct {
	TimeLimit time.Time
	Count     int
	Length    float64
}

var (
	en = control.AutoRegister(&ctrl.Options[*zero.Ctx]{
		DisableOnDefault: false,
		Brief:            "牛牛大作战",
		Help: "- 打胶\n" +
			"- 使用[道具名称]打胶\n" +
			"- jj@xxx\n" +
			"- 使用[道具名称]jj@xxx\n" +
			"- 注册牛牛\n" +
			"- 赎牛牛(cd:60分钟)\n" +
			"- 出售牛牛\n" +
			"- 牛牛拍卖行\n" +
			"- 牛牛商店\n" +
			"- 牛牛背包\n" +
			"- 注销牛牛\n" +
			"- 查看我的牛牛\n" +
			"- 牛子长度排行\n" +
			"- 牛子深度排行\n" +
			"\n ps : 出售后的牛牛都会进入牛牛拍卖行哦",
		PrivateDataFolder: "niuniu",
	})
	dajiaoLimiter = rate.NewManager[string](time.Second*90, 1)
	jjLimiter     = rate.NewManager[string](time.Second*150, 1)
	jjCount       = syncx.Map[string, *lastLength]{}
	register      = syncx.Map[string, *lastLength]{}
)

func init() {
	en.OnFullMatch("牛牛拍卖行", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		uid := ctx.Event.UserID
		auction, err := niu.ShowAuction(gid)
		if err != nil {
			ctx.SendChain(message.Text("ERROR:", err))
			return
		}

		var messages message.Message
		messages = append(messages, ctxext.FakeSenderForwardNode(ctx, message.Text("牛牛拍卖行有以下牛牛")))
		for _, info := range auction {
			msg := fmt.Sprintf("商品序号: %d\n牛牛原所属: %d\n牛牛价格: %d%s\n牛牛大小: %.2fcm",
				info.ID+1, info.UserID, info.Money, wallet.GetWalletName(), info.Length)
			messages = append(messages, ctxext.FakeSenderForwardNode(ctx, message.Text(msg)))
		}
		if id := ctx.Send(messages).ID(); id == 0 {
			ctx.Send(message.Text("发送拍卖行失败"))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.Message), message.Text("请输入对应序号进行购买"))
		recv, cancel := zero.NewFutureEvent("message", 999, false, zero.CheckUser(uid), zero.CheckGroup(gid), zero.RegexRule(`^(\d+)$`)).Repeat()
		defer cancel()
		timer := time.NewTimer(120 * time.Second)
		answer := ""
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				ctx.SendChain(message.At(uid), message.Text(" 超时,已自动取消"))
				return
			case r := <-recv:
				answer = r.Event.Message.String()
				n, err := strconv.Atoi(answer)
				if err != nil {
					ctx.SendChain(message.Text("ERROR: ", err))
					return
				}
				n--
				msg, err := niu.Auction(gid, uid, n)
				if err != nil {
					ctx.SendChain(message.Text("ERROR:", err))
					return
				}
				ctx.SendChain(message.Reply(ctx.Event.Message), message.Text(msg))
				return
			}
		}
	})
	en.OnFullMatch("出售牛牛", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		uid := ctx.Event.UserID

		// 在出售牛牛前，检查用户是否有jjCount记录，如果有则清除
		jjKey := fmt.Sprintf("%d_%d", gid, uid)
		if _, ok := jjCount.Load(jjKey); ok {
			// 用户有jjCount记录，清除它
			jjCount.Delete(jjKey)
		}

		sell, err := niu.Sell(gid, uid)
		if err != nil {
			ctx.SendChain(message.Text("ERROR:", err))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(sell))
	})
	en.OnFullMatch("牛牛背包", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		uid := ctx.Event.UserID
		bag, err := niu.Bag(gid, uid)
		if err != nil {
			ctx.SendChain(message.Text("ERROR:", err))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(bag))
	})
	en.OnFullMatch("牛牛商店", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		uid := ctx.Event.UserID

		if _, err := niu.GetWordNiuNiu(gid, uid); err != nil {
			ctx.SendChain(message.Text(niu.ErrNoNiuNiu))
			return
		}

		propMap := map[int]struct {
			name        string
			cost        int
			scope       string
			description string
			count       int
		}{
			1: {"伟哥", 300, "打胶", "可以让你打胶每次都增长", 5},
			2: {"媚药", 300, "打胶", "可以让你打胶每次都减少", 5},
			3: {"击剑神器", 500, "jj", "可以让你每次击剑都立于不败之地", 2},
			4: {"击剑神稽", 500, "jj", "可以让你每次击剑都失败", 2},
		}

		var messages message.Message
		messages = append(messages, ctxext.FakeSenderForwardNode(ctx, message.Text("牛牛商店当前售卖的物品如下")))
		for id := range propMap {
			product := propMap[id]
			productInfo := fmt.Sprintf("商品%d\n商品名: %s\n商品价格: %dATRI币\n商品作用域: %s\n商品描述: %s\n使用次数:%d",
				id, product.name, product.cost, product.scope, product.description, product.count)
			messages = append(messages, ctxext.FakeSenderForwardNode(ctx, message.Text(productInfo)))
		}
		if id := ctx.Send(messages).ID(); id == 0 {
			ctx.Send(message.Text("发送商店失败"))
			return
		}

		ctx.SendChain(message.Text("输入对应序号进行购买商品"))
		recv, cancel := zero.NewFutureEvent("message", 999, false, zero.CheckUser(uid), zero.CheckGroup(gid), zero.RegexRule(`^(\d+)$`)).Repeat()
		defer cancel()
		timer := time.NewTimer(120 * time.Second)
		answer := ""
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				ctx.SendChain(message.At(uid), message.Text(" 超时,已自动取消"))
				return
			case r := <-recv:
				answer = r.Event.Message.String()
				n, err := strconv.Atoi(answer)
				if err != nil {
					ctx.SendChain(message.Text("ERROR: ", err))
					return
				}

				if err = fixedStore(gid, uid, n); err != nil {
					ctx.SendChain(message.Text("ERROR: ", err))
					return
				}

				ctx.SendChain(message.Text("购买成功!"))
				return
			}
		}
	})
	en.OnFullMatch("赎牛牛", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		uid := ctx.Event.UserID

		// 首先检查用户是否拥有牛牛
		_, err := niu.GetWordNiuNiu(gid, uid)
		if err != nil {
			ctx.SendChain(message.Text("你还没有牛牛，请先注册牛牛"))
			return
		}

		// 检查用户是否有jjCount记录
		jjKey := fmt.Sprintf("%d_%d", gid, uid)
		last, ok := jjCount.Load(jjKey)

		if !ok {
			ctx.SendChain(message.Text("你还没有被厥呢"))
			return
		}

		if time.Since(last.TimeLimit) > time.Hour {
			ctx.SendChain(message.Text("时间已经过期了,牛牛已被收回!"))
			jjCount.Delete(jjKey)
			return
		}

		if last.Count < 4 {
			ctx.SendChain(message.Text("你还没有被厥够4次呢,不能赎牛牛"))
			return
		}

		// 检查用户是否有足够的钱
		money := wallet.GetWalletOf(uid)
		if money < 150 {
			ctx.SendChain(message.Text(fmt.Sprintf("赎牛牛需要150%s，你只有%d%s，快去赚钱吧！",
				wallet.GetWalletName(), money, wallet.GetWalletName())))
			return
		}

		ctx.SendChain(message.Text("再次确认一下哦,这次赎牛牛需要支付150", wallet.GetWalletName(),
			"，牛牛长度将会变成", last.Length, "cm\n还需要嘛【是|否】"))
		recv, cancel := zero.NewFutureEvent("message", 999, false, zero.CheckUser(uid), zero.CheckGroup(gid), zero.RegexRule(`^(是|否)$`)).Repeat()
		defer cancel()
		timer := time.NewTimer(2 * time.Minute)
		defer timer.Stop()
		for {
			select {
			case <-timer.C:
				ctx.SendChain(message.Text("操作超时，已自动取消"))
				return
			case c := <-recv:
				answer := c.Event.Message.String()
				if answer == "否" {
					ctx.SendChain(message.Text("取消成功!"))
					return
				}

				// 再次检查用户是否有足够的钱（可能在确认过程中花掉了）
				money = wallet.GetWalletOf(uid)
				if money < 150 {
					ctx.SendChain(message.Text(fmt.Sprintf("赎牛牛需要150%s，你只有%d%s，快去赚钱吧！",
						wallet.GetWalletName(), money, wallet.GetWalletName())))
					return
				}

				// 扣除用户的钱
				if err := wallet.InsertWalletOf(uid, -150); err != nil {
					ctx.SendChain(message.Text("ERROR: 扣除金钱失败:", err))
					return
				}

				// 设置用户的牛牛长度
				currentLength, err := niu.GetWordNiuNiu(gid, uid)
				if err != nil {
					ctx.SendChain(message.Text("ERROR: 获取当前牛牛长度失败:", err))
					// 退还用户的钱
					wallet.InsertWalletOf(uid, 150)
					return
				}

				// 计算需要增加或减少的长度差值
				lengthDiff := last.Length - currentLength

				if err := niu.SetWordNiuNiu(gid, uid, lengthDiff); err != nil {
					ctx.SendChain(message.Text("ERROR: 设置牛牛长度失败:", err))
					// 如果设置失败，退还用户的钱
					wallet.InsertWalletOf(uid, 150)
					return
				}

				// 删除jjCount记录
				jjCount.Delete(jjKey)

				ctx.SendChain(message.At(uid), message.Text(fmt.Sprintf("恭喜你!成功赎回牛牛,当前长度为:%.2fcm", last.Length)))
				return
			}
		}
	})
	en.OnFullMatch("牛子长度排行", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		infos, err := niu.GetRankingInfo(gid, true)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		img, err := processRankingImg(infos, ctx, true)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.ImageBytes(img))
	})
	en.OnFullMatch("牛子深度排行", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		infos, err := niu.GetRankingInfo(gid, false)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		img, err := processRankingImg(infos, ctx, false)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.ImageBytes(img))
	})
	en.OnFullMatch("查看我的牛牛", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		uid := ctx.Event.UserID
		gid := ctx.Event.GroupID
		view, err := niu.View(gid, uid, ctx.CardOrNickName(uid))
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(view))
	})
	en.OnRegex(`^(?:.*使用(.*))??打胶$`, zero.OnlyGroup).SetBlock(true).Limit(func(ctx *zero.Ctx) *rate.Limiter {
		lt := dajiaoLimiter.Load(fmt.Sprintf("%d_%d", ctx.Event.GroupID, ctx.Event.UserID))
		ctx.State["dajiao_last_touch"] = lt.LastTouch()
		return lt
	}, func(ctx *zero.Ctx) {
		timePass := int(time.Since(time.Unix(ctx.State["dajiao_last_touch"].(int64), 0)).Seconds())
		ctx.SendChain(message.Text(randomChoice([]string{
			fmt.Sprintf("才过去了%ds时间,你就又要打🦶了，身体受得住吗", timePass),
			fmt.Sprintf("不行不行，你的身体会受不了的，歇%ds再来吧", 90-timePass),
			fmt.Sprintf("休息一下吧，会炸膛的！%ds后再来吧", 90-timePass),
			fmt.Sprintf("打咩哟，你的牛牛会爆炸的，休息%ds再来吧", 90-timePass),
		})))
	}).Handle(func(ctx *zero.Ctx) {
		// 获取群号和用户ID
		gid := ctx.Event.GroupID
		uid := ctx.Event.UserID
		fiancee := ctx.State["regex_matched"].([]string)

		msg, err := niu.HitGlue(gid, uid, fiancee[1])
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			dajiaoLimiter.Delete(fmt.Sprintf("%d_%d", ctx.Event.GroupID, ctx.Event.UserID))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(msg))
	})
	en.OnFullMatch("注册牛牛", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		gid := ctx.Event.GroupID
		uid := ctx.Event.UserID
		msg, err := niu.Register(gid, uid)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(msg))
	})
	en.OnMessage(zero.NewPattern(nil).Text(`^(?:.*使用(.*))??jj`).At().AsRule(),
		zero.OnlyGroup).SetBlock(true).Limit(func(ctx *zero.Ctx) *rate.Limiter {
		lt := jjLimiter.Load(fmt.Sprintf("%d_%d", ctx.Event.GroupID, ctx.Event.UserID))
		ctx.State["jj_last_touch"] = lt.LastTouch()
		return lt
	}, func(ctx *zero.Ctx) {
		timePass := int(time.Since(time.Unix(ctx.State["jj_last_touch"].(int64), 0)).Seconds())
		ctx.SendChain(message.Text(randomChoice([]string{
			fmt.Sprintf("才过去了%ds时间,你就又要击剑了，真是饥渴难耐啊", timePass),
			fmt.Sprintf("不行不行，你的身体会受不了的，歇%ds再来吧", 150-timePass),
			fmt.Sprintf("你这种男同就应该被送去集中营！等待%ds再来吧", 150-timePass),
			fmt.Sprintf("打咩哟！你的牛牛会炸的，休息%ds再来吧", 150-timePass),
		})))
	},
	).Handle(func(ctx *zero.Ctx) {
		patternParsed := ctx.State[zero.KeyPattern].([]zero.PatternParsed)
		adduser, err := strconv.ParseInt(patternParsed[1].At(), 10, 64)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			jjLimiter.Delete(fmt.Sprintf("%d_%d", ctx.Event.GroupID, ctx.Event.UserID))
			return
		}
		uid := ctx.Event.UserID
		gid := ctx.Event.GroupID
		msg, length, err := niu.JJ(gid, uid, adduser, patternParsed[0].Text()[1])
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			jjLimiter.Delete(fmt.Sprintf("%d_%d", ctx.Event.GroupID, ctx.Event.UserID))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(msg))
		j := fmt.Sprintf("%d_%d", gid, adduser)
		count, ok := jjCount.Load(j)
		var c lastLength
		// 按照最后一次被jj时的时间计算，超过60分钟则重置
		if !ok {
			c = lastLength{
				TimeLimit: time.Now(),
				Count:     1,
				Length:    length,
			}
		} else {
			c = lastLength{
				TimeLimit: time.Now(),
				Count:     count.Count + 1,
				Length:    count.Length,
			}
			if time.Since(c.TimeLimit) > time.Hour {
				c = lastLength{
					TimeLimit: time.Now(),
					Count:     1,
					Length:    length,
				}
			}
		}

		jjCount.Store(j, &c)
		if c.Count > 2 {
			ctx.SendChain(message.Text(randomChoice([]string{
				fmt.Sprintf("你们太厉害了，对方已经被你们打了%d次了，你们可以继续找他🤺", c.Count),
				"你们不要再找ta🤺啦！"},
			)))

			if c.Count >= 4 {
				id := ctx.SendPrivateMessage(adduser,
					message.Text(fmt.Sprintf("你在%d群里已经被厥冒烟了，快去群里赎回你原本的牛牛!\n发送:`赎牛牛`即可！", gid)))
				if id == 0 {
					ctx.SendChain(message.At(adduser), message.Text("快发送`赎牛牛`来赎回你原本的牛牛!"))
				}
			}
		}
	})
	en.OnFullMatch("注销牛牛", zero.OnlyGroup).SetBlock(true).Handle(func(ctx *zero.Ctx) {
		uid := ctx.Event.UserID
		gid := ctx.Event.GroupID
		key := fmt.Sprintf("%d_%d", gid, uid)

		// 在注销牛牛前，检查用户是否有jjCount记录，如果有则清除
		if _, ok := jjCount.Load(key); ok {
			// 用户有jjCount记录，清除它
			jjCount.Delete(key)
		}

		data, ok := register.Load(key)
		switch {
		case !ok || time.Since(data.TimeLimit) > time.Hour*12:
			data = &lastLength{
				TimeLimit: time.Now(),
				Count:     1,
			}
		default:
			if err := wallet.InsertWalletOf(uid, -data.Count*50); err != nil {
				ctx.SendChain(message.Text("你的钱不够你注销牛牛了，这次注销需要", data.Count*50, wallet.GetWalletName()))
				return
			}
		}
		register.Store(key, data)
		msg, err := niu.Cancel(gid, uid)
		if err != nil {
			ctx.SendChain(message.Text("ERROR: ", err))
			return
		}
		ctx.SendChain(message.Reply(ctx.Event.MessageID), message.Text(msg))
	})
}

// fixedStore is a wrapper around niu.Store that fixes the bug where items are stored in the wrong table
func fixedStore(gid, uid int64, n int) error {
	// Define the user info struct to match the database schema
	type userInfoStruct struct {
		UID       int64   `db:"uid"`
		Length    float64 `db:"length"`
		UserCount int     `db:"usercount"`
		WeiGe     int     `db:"weige"`
		Philter   int     `db:"philter"`
		Artifact  int     `db:"artifact"`
		ShenJi    int     `db:"shenji"`
		Buff1     int     `db:"buff1"`
		Buff2     int     `db:"buff2"`
		Buff3     int     `db:"buff3"`
		Buff4     int     `db:"buff4"`
		Buff5     int     `db:"buff5"`
	}

	// 检查用户是否存在
	_, err := niu.GetWordNiuNiu(gid, uid)
	if err != nil {
		return err
	}

	// Get the current inventory to check what items the user has
	bagBefore, err := niu.Bag(gid, uid)
	if err != nil {
		return err
	}

	// Call the original Store function to process the purchase
	if err := niu.Store(gid, uid, n); err != nil {
		return err
	}

	// Check if the inventory was updated correctly
	bagAfter, err := niu.Bag(gid, uid)
	if err != nil {
		return err
	}

	// If the bags are the same, it means the items weren't added to the inventory
	if bagBefore == bagAfter {
		// 打开数据库
		db := sql.New("data/niuniu/niuniu.db")
		if err := db.Open(time.Hour); err != nil {
			return err
		}
		defer db.Close()

		// 检查是否存在错误的表（使用uid而不是gid）
		wrongTableName := strconv.FormatInt(uid, 10)
		correctTableName := strconv.FormatInt(gid, 10)

		// 获取所有表
		tables, err := db.ListTables()
		if err != nil {
			return err
		}

		// 检查错误的表是否存在
		wrongTableExists := false
		for _, table := range tables {
			if table == wrongTableName {
				wrongTableExists = true
				break
			}
		}

		// 如果错误的表存在，尝试从中获取数据
		var wrongUserInfo userInfoStruct
		if wrongTableExists {
			if err := db.Find(wrongTableName, &wrongUserInfo, "WHERE UID = ?", uid); err == nil {
				// 找到了错误表中的数据，删除它
				if err := db.Del(wrongTableName, "WHERE UID = ?", uid); err != nil {
					return err
				}
			}
		}

		// 从正确的表中获取用户信息
		var userInfo userInfoStruct
		if err := db.Find(correctTableName, &userInfo, "WHERE UID = ?", uid); err != nil {
			// 如果在正确的表中找不到用户，创建一个新的用户信息
			userInfo = userInfoStruct{
				UID:    uid,
				Length: 0, // 默认长度，可以根据需要调整
			}
		}

		// 根据购买的物品更新用户信息
		switch n {
		case 1: // 伟哥
			userInfo.WeiGe += 5
		case 2: // 媚药
			userInfo.Philter += 5
		case 3: // 击剑神器
			userInfo.Artifact += 2
		case 4: // 击剑神稽
			userInfo.ShenJi += 2
		}

		// 保存更新后的用户信息到正确的表
		if err := db.Insert(correctTableName, &userInfo); err != nil {
			return err
		}
	}

	return nil
}

func randomChoice(options []string) string {
	return options[rand.Intn(len(options))]
}
