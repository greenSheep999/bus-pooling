// reconcile · 交叉对账（docs/23-endpoints-todo §1）· 拿我方 pull_round 跟 vendor 侧账本核对。
//
// 生产开 dry_run=false 真实扣费后 · 定期跑这条查"被多扣 / 漏退 / 幻扣"。
// 只读 · 不改任何表 · 打印差异清单 + 汇总。
//
// 用法：bus-pooling reconcile [天数]   （默认近 30 天）

package main

import (
	"context"
	"fmt"

	"github.com/bus-pooling/bus-pooling/internal/config"
	"github.com/bus-pooling/bus-pooling/internal/db"
	"github.com/bus-pooling/bus-pooling/internal/vendorview"
)

func runReconcile(ctx context.Context, cfg config.Config, args []string) error {
	days := 30
	if len(args) > 0 && args[0] != "" {
		if _, err := fmt.Sscanf(args[0], "%d", &days); err != nil || days <= 0 {
			return fmt.Errorf("天数要正整数（例 reconcile 7）")
		}
	}

	database, err := db.Open(ctx, cfg.DB.Path)
	if err != nil {
		return err
	}
	defer database.Close()

	rec := vendorview.NewReconciler(database.DB, vendorview.NewLedgerStore(database.DB))
	discs, sum, err := rec.Reconcile(ctx, days)
	if err != nil {
		return err
	}

	fmt.Printf("对账窗口：近 %d 天 · 核对 round %d 笔 · 差异 %d 条\n",
		days, sum.RoundsChecked, sum.Discrepancies)
	if sum.Discrepancies == 0 {
		fmt.Println("✓ 全部对得上（或暂无成交 round）")
		return nil
	}
	fmt.Println("按类型：")
	for kind, n := range sum.ByKind {
		fmt.Printf("  %-16s %d\n", kind, n)
	}
	fmt.Println("\n明细：")
	for _, d := range discs {
		fmt.Printf("  [%s] vendor=%s order=%s round=%s\n      %s\n",
			d.Kind, d.VendorID, d.OrderID, d.RoundID, d.Detail)
	}
	// 有差异返非 nil · 让运维脚本 / CI 能靠 exit code 感知
	return fmt.Errorf("发现 %d 条对账差异 · 见上", sum.Discrepancies)
}
