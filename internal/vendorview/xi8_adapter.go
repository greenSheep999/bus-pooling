package vendorview

// xi8_adapter · 让 xi8.Backfiller 通过窄接口把逐 zone 单价直接落 vendor_probe_zone。
//
// 单独文件是因为 InsertZoneBatch 用 xi8.ZoneSample · 得 import xi8。
// vendorview → xi8 单向依赖 · 无循环（xi8 只依赖 providers）。

import (
	"context"

	"github.com/bus-pooling/bus-pooling/internal/xi8"
)

// InsertZoneBatch · 实现 xi8.ZoneStore 接口 · source 标 'xi8'
func (s *ProbeZoneStore) InsertZoneBatch(ctx context.Context, samples []xi8.ZoneSample) error {
	if len(samples) == 0 {
		return nil
	}
	converted := make([]ProbeZoneSample, 0, len(samples))
	for _, x := range samples {
		converted = append(converted, ProbeZoneSample{
			VendorID:       x.VendorID,
			ProbedAt:       x.ProbedAt,
			Zone:           x.Zone,
			Region:         x.Region,
			Available:      x.Available,
			VendorCurrency: x.VendorCurrency,
			VendorUnitRaw:  x.VendorUnitRaw,
			OurUnitCredits: x.OurUnitCredits,
			OurUnitSource:  "xi8_native",
			// Source 由上游指定（"xi8" 实时快照 / "xi8_notif" 历史通知）· 空按 "xi8"
			Source: firstNonEmpty(x.Source, "xi8"),
		})
	}
	return s.InsertBatch(ctx, converted)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
