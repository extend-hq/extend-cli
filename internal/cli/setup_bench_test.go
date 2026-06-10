package cli

import (
	"context"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// BenchmarkSetupFrame measures one animation frame (tick Update + View)
// of the setup wizard at a typical terminal size, after the intro has
// settled into the idle shimmer.
func BenchmarkSetupFrame(b *testing.B) {
	m := newSetupModel(context.Background(), true, "us", func(context.Context, string, string, string) error { return nil })
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	// Run the intro to completion so we're benchmarking the idle state.
	for i := 0; i < 600; i++ {
		model, _ = model.Update(logoTickMsg{})
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		model, _ = model.Update(logoTickMsg{})
		_ = model.(setupModel).View()
	}
}

// BenchmarkSetupView isolates View (compose + render) from Update.
func BenchmarkSetupView(b *testing.B) {
	m := newSetupModel(context.Background(), true, "us", func(context.Context, string, string, string) error { return nil })
	var model tea.Model = m
	model, _ = model.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	for i := 0; i < 600; i++ {
		model, _ = model.Update(logoTickMsg{})
	}
	sm := model.(setupModel)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = sm.View()
	}
}
