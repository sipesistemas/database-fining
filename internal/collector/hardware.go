package collector

import (
	"runtime"

	"github.com/hugomesquita/database-fining/internal/model"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
)

// CollectHardware reads CPU, memory and disk facts from the local host.
// dataPath is the directory ClickHouse stores data in; disk stats are read
// from the filesystem containing it. Errors from individual probes are
// tolerated so a partial picture is still returned.
func CollectHardware(dataPath string) model.Hardware {
	hw := model.Hardware{
		LogicalCPUs: runtime.NumCPU(),
		DiskPath:    dataPath,
	}

	if counts, err := cpu.Counts(false); err == nil && counts > 0 {
		hw.PhysicalCPUs = counts
	} else {
		hw.PhysicalCPUs = hw.LogicalCPUs
	}

	if vm, err := mem.VirtualMemory(); err == nil {
		hw.TotalMemory = vm.Total
		hw.FreeMemory = vm.Available
	}

	if du, err := disk.Usage(dataPath); err == nil {
		hw.DiskTotal = du.Total
		hw.DiskFree = du.Free
	}

	if avg, err := load.Avg(); err == nil {
		hw.LoadAvg1 = avg.Load1
	}

	return hw
}
