// Package mcfish 钓鱼模拟器
package mcfish

import (
	"github.com/FloatTech/zbputils/ctxext"
	zero "github.com/wdvxdr1123/ZeroBot"
	"github.com/wdvxdr1123/ZeroBot/message"
)

func init() {
	engine.OnFullMatch("钓鱼数据库升级", getdb).SetBlock(true).Limit(ctxext.LimitByUser).Handle(func(ctx *zero.Ctx) {
		// 检查是否为管理员
		if !zero.AdminPermission(ctx) {
			ctx.SendChain(message.Text("只有管理员才能执行数据库升级操作"))
			return
		}

		ctx.SendChain(message.Text("开始升级钓鱼数据库..."))

		// 创建临时表
		err := dbdata.db.Exec("CREATE TABLE IF NOT EXISTS equips_new (ID INTEGER, Equip TEXT, Durable INTEGER, Maintenance INTEGER, Induce INTEGER, Favor INTEGER, Durability INTEGER DEFAULT 0, ExpRepair INTEGER DEFAULT 0)")
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at migrate.go.1]:", err))
			return
		}

		// 复制数据到新表，并添加新列
		err = dbdata.db.Exec("INSERT INTO equips_new SELECT ID, Equip, Durable, Maintenance, Induce, Favor, 0, 0 FROM equips")
		if err != nil {
			// 如果表不存在，忽略错误
			ctx.SendChain(message.Text("没有找到现有的装备数据，将创建新表"))
		}

		// 删除旧表
		err = dbdata.db.Exec("DROP TABLE IF EXISTS equips")
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at migrate.go.2]:", err))
			return
		}

		// 重命名新表
		err = dbdata.db.Exec("ALTER TABLE equips_new RENAME TO equips")
		if err != nil {
			ctx.SendChain(message.Text("[ERROR at migrate.go.3]:", err))
			return
		}

		ctx.SendChain(message.Text("钓鱼数据库升级完成！"))
	})
}
