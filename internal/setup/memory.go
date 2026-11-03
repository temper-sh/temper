package setup

import (
	"fmt"

	"github.com/temper-sh/temper/internal/budget"
	"github.com/temper-sh/temper/internal/machine"
)

// WiredMemoryAdvice is a setup recommendation, not a measured runtime footprint
// or permission to change the machine. It never changes preparation eligibility.
type WiredMemoryAdvice struct {
	RequiredMiB       int64  `json:"required_mib"`
	LimitMiB          int64  `json:"limit_mib"`
	LimitSource       string `json:"limit_source"`
	PhysicalMiB       int64  `json:"physical_mib"`
	SuggestedLimitMiB int64  `json:"suggested_limit_mib,omitempty"`
}

func wiredMemoryAdvice(requiredMiB int64, facts machine.Facts, prediction budget.Prediction) *WiredMemoryAdvice {
	// Leave a visible margin above the existing admission prediction. This is
	// advisory policy; it is not an estimate of KV cache or engine overhead.
	margin := max(int64(2048), (facts.WiredLimitMiB+9)/10)
	if requiredMiB == 0 || facts.WiredLimitMiB-requiredMiB >= margin {
		return nil
	}
	a := &WiredMemoryAdvice{RequiredMiB: requiredMiB, LimitMiB: facts.WiredLimitMiB,
		LimitSource: facts.WiredLimitSource, PhysicalMiB: facts.PhysicalMemoryBytes / MiB}
	// Suggest whole GiB, leaving at least 4 GiB and 15% of RAM outside the GPU
	// limit for macOS and other applications. Do not recommend an increase when
	// that reserve prevents the selected setup from gaining the advisory margin.
	ceiling := min(a.PhysicalMiB-4096, a.PhysicalMiB*85/100)
	if requiredMiB <= ceiling {
		// Leave 10% of the proposed limit, not of the old limit, so accepting
		// the advice does not immediately produce another increase request.
		target := max(requiredMiB+2048, (requiredMiB*10+8)/9)
		target = (target + 1023) / 1024 * 1024
		for ; target > a.LimitMiB && target <= ceiling; target += 1024 {
			proposedRequired := requiredMiB
			if prediction.Holder != "" {
				// The fraction envelope grows with the new Metal budget too.
				// Keep the fixed model/OS lower bound and account for that growth
				// before recommending a value that would still be too tight.
				proposedRequired = max(proposedRequired, int64(prediction.Utilization*float64(target))+prediction.CoTenantsMiB+prediction.OSFloorMiB)
			}
			if target-proposedRequired >= max(int64(2048), (target+9)/10) {
				a.SuggestedLimitMiB = target
				break
			}
		}
	}
	return a
}

func (a WiredMemoryAdvice) Summary() string {
	if a.SuggestedLimitMiB > 0 {
		if a.RequiredMiB <= a.LimitMiB {
			return fmt.Sprintf("Optional: increase the wired-memory allowance from %s to %s (%d MiB) for additional headroom. The current wired-memory allowance does not block preparation.", Size(a.LimitMiB*MiB), Size(a.SuggestedLimitMiB*MiB), a.SuggestedLimitMiB)
		}
		return fmt.Sprintf("Accounted memory exceeds the current wired-memory allowance. Consider increasing it to %s (%d MiB), choosing a smaller model or reducing Context.", Size(a.SuggestedLimitMiB*MiB), a.SuggestedLimitMiB)
	}
	if a.RequiredMiB <= a.LimitMiB {
		return "The current wired-memory allowance does not block preparation, but has less headroom than Temper recommends. There is too little room to suggest a higher allowance while reserving memory for macOS and other apps."
	}
	return "There is too little room to recommend a higher wired-memory limit while reserving memory for macOS and other apps. Choose a smaller model or reduce Context."
}

func (a WiredMemoryAdvice) Section() Section {
	lines := []string{a.Summary(),
		fmt.Sprintf("Accounted memory plus OS allowance: %s; current GPU budget: %s (Metal recommended working set).", Size(a.RequiredMiB*MiB), Size(a.LimitMiB*MiB)),
		"Suggested limits leave at least 4 GiB and 15% of RAM for macOS and other apps. Headroom is recommendation policy, not a measured runtime requirement or a macOS maximum.",
	}
	if spare := a.LimitMiB - a.RequiredMiB; spare < 0 {
		lines = append(lines, "The accounted memory exceeds the limit by "+Size(-spare*MiB)+".")
	} else {
		lines = append(lines, "Only "+Size(spare*MiB)+" remains below the wired limit.")
	}
	if a.SuggestedLimitMiB > 0 {
		manual := "[manual] To increase the wired-memory allowance, open another Terminal and note the current value:"
		if a.RequiredMiB <= a.LimitMiB {
			manual = "[manual] Optional: increase the wired-memory allowance. In another Terminal, first note the current value:"
		}
		lines = append(lines,
			manual,
			"sysctl -n iogpu.wired_limit_mb",
			"If that setting is available, run (requires your administrator password):",
			fmt.Sprintf("sudo sysctl iogpu.wired_limit_mb=%d", a.SuggestedLimitMiB),
			"Verify the effective budget with temper machine facts: wired_limit_mib must show the intended budget with wired_limit_source: live-metal. A changed sysctl value alone does not verify the Metal budget.",
			"Then quit and rerun the same Temper setup command to refresh machine facts. If Metal still reports the old budget, do not keep raising the override.",
			"This change lasts until reboot. To undo sooner, stop the model and restore the value you noted with sudo sysctl iogpu.wired_limit_mb=<previous-value>.",
			"The suggested limit leaves "+Size((a.PhysicalMiB-a.SuggestedLimitMiB)*MiB)+" outside the GPU allowance. Other apps may need more; watch Memory Pressure in Activity Monitor.",
		)
	}
	lines = append(lines, "Raising the limit adds no physical RAM. Catalog measurements or supported estimates are included, but unmeasured workloads and allocations remain; this recommendation does not establish runtime fit.")
	title := "Wired-memory budget exceeded"
	if a.RequiredMiB <= a.LimitMiB {
		title = "Optional wired-memory headroom"
	}
	return Section{Title: title, Lines: lines, Warning: true}
}
