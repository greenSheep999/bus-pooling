package decider

import (
	"errors"
	"testing"
)

// 数量在区间内 → 放行
func TestLimits_CountInRange(t *testing.T) {
	l := Limits{MinCount: 1, MaxCount: 50}
	for _, n := range []int{1, 3, 25, 50} {
		if err := l.checkCountRange(n); err != nil {
			t.Errorf("count=%d 该放行 · got=%v", n, err)
		}
	}
}

// 低于下限 / 超过上限 → 拒（不静默截断）
func TestLimits_CountOutOfRange(t *testing.T) {
	l := Limits{MinCount: 2, MaxCount: 10}
	for _, n := range []int{1, 11, 999} {
		err := l.checkCountRange(n)
		if !errors.Is(err, ErrCountOutOfRange) {
			t.Errorf("count=%d 该拒 · got=%v", n, err)
		}
	}
}

// 区间数字能取出来给 api 层拼人话（不透内部 err 字符串）
func TestCountRangeOf_ExtractsBounds(t *testing.T) {
	l := Limits{MinCount: 2, MaxCount: 10}
	err := l.checkCountRange(99)
	lo, hi, ok := CountRangeOf(err)
	if !ok {
		t.Fatal("该能取出区间")
	}
	if lo != 2 || hi != 10 {
		t.Errorf("区间 = [%d,%d] · want [2,10]", lo, hi)
	}
}

// 非区间错误取不出数字
func TestCountRangeOf_OtherErrorReturnsFalse(t *testing.T) {
	if _, _, ok := CountRangeOf(ErrNoStock); ok {
		t.Error("非区间错误不该返 ok=true")
	}
	if _, _, ok := CountRangeOf(nil); ok {
		t.Error("nil 不该返 ok=true")
	}
}

// 0 = 不限（老装配 / 测试兼容 —— 零值 Limits 不该挡任何东西）
func TestLimits_ZeroMeansUnlimited(t *testing.T) {
	var l Limits // 零值
	for _, n := range []int{1, 1000, 99999} {
		if err := l.checkCountRange(n); err != nil {
			t.Errorf("零值 Limits 不该限 count=%d · got=%v", n, err)
		}
	}
}

// 只设上限不设下限 · 只设下限不设上限 都要正常工作
func TestLimits_PartialBounds(t *testing.T) {
	onlyMax := Limits{MaxCount: 5}
	if err := onlyMax.checkCountRange(1); err != nil {
		t.Errorf("只设上限时 count=1 该放行 · got=%v", err)
	}
	if err := onlyMax.checkCountRange(6); !errors.Is(err, ErrCountOutOfRange) {
		t.Errorf("只设上限时 count=6 该拒 · got=%v", err)
	}

	onlyMin := Limits{MinCount: 3}
	if err := onlyMin.checkCountRange(2); !errors.Is(err, ErrCountOutOfRange) {
		t.Errorf("只设下限时 count=2 该拒 · got=%v", err)
	}
	if err := onlyMin.checkCountRange(9999); err != nil {
		t.Errorf("只设下限时大数该放行 · got=%v", err)
	}
}

// 限流维度只接受白名单列名（防 SQL 注入 —— 列名不能来自外部输入）
func TestCountInFlight_RejectsBadColumn(t *testing.T) {
	if _, err := countInFlight(nil, nil, "passenger_id; DROP TABLE bus", "x"); err == nil {
		t.Error("非法列名该报错")
	}
	if _, err := countInFlight(nil, nil, "balance", "x"); err == nil {
		t.Error("非白名单列名该报错")
	}
}

// 在飞状态清单不该含终态 —— 含了会永久占用并发额度
func TestInFlightStatuses_ExcludesTerminal(t *testing.T) {
	terminal := map[Status]bool{
		StatusCompleted:        true,
		StatusCancelledReserve: true,
		StatusNeedManual:       true,
	}
	for _, s := range inFlightStatuses {
		if terminal[s] {
			t.Errorf("终态 %q 不该算在飞（会永久占额度）", s)
		}
	}
	if len(inFlightStatuses) == 0 {
		t.Error("在飞状态清单不该为空")
	}
}
