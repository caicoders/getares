// Copyright (C) 2026 caicoders (https://github.com/caicoders)
// SPDX-License-Identifier: AGPL-3.0-or-later

package wizard

import (
	"fmt"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"github.com/shirou/gopsutil/v3/mem"
)

// HardwareInfo holds detected system capabilities.
type HardwareInfo struct {
	TotalRAMMB uint64
	GPUName    string
	GPUVRAMMB  uint64
	HasGPU     bool
	OS         string
}

// ModelSuggestion is a recommended model based on available hardware.
type ModelSuggestion struct {
	Name     string
	ID       string
	VRAMMBRequired uint64
	RAMMBRequired  uint64
	URL      string
	Quality  string // ★ rating
}

// DetectHardware inspects the current machine's RAM and GPU.
func DetectHardware() HardwareInfo {
	info := HardwareInfo{OS: runtime.GOOS}

	// RAM — gopsutil works on Windows, Linux, macOS without syscalls
	if v, err := mem.VirtualMemory(); err == nil {
		info.TotalRAMMB = v.Total / 1024 / 1024
	}

	// GPU — nvidia-smi works on Windows and Linux with NVIDIA drivers
	// macOS Apple Silicon is detected separately
	if name, vram, err := detectNvidiaGPU(); err == nil {
		info.GPUName = name
		info.GPUVRAMMB = vram
		info.HasGPU = true
	} else if runtime.GOOS == "darwin" {
		if name, vram := detectAppleSilicon(); name != "" {
			info.GPUName = name
			info.GPUVRAMMB = vram
			info.HasGPU = true
		}
	}

	return info
}

func detectNvidiaGPU() (name string, vramMB uint64, err error) {
	out, err := exec.Command(
		"nvidia-smi",
		"--query-gpu=name,memory.total",
		"--format=csv,noheader,nounits",
	).Output()
	if err != nil {
		return "", 0, err
	}

	// Output: "NVIDIA GeForce RTX 3060 Ti, 6144"
	line := strings.TrimSpace(string(out))
	parts := strings.Split(line, ", ")
	if len(parts) != 2 {
		return "", 0, fmt.Errorf("unexpected nvidia-smi output: %s", line)
	}

	name = strings.TrimSpace(parts[0])
	vram, err := strconv.ParseUint(strings.TrimSpace(parts[1]), 10, 64)
	if err != nil {
		return "", 0, err
	}
	return name, vram, nil
}

func detectAppleSilicon() (name string, unifiedMemoryMB uint64) {
	// On Apple Silicon, GPU and CPU share unified memory.
	// We report total RAM as usable "VRAM".
	out, err := exec.Command(
		"system_profiler", "SPHardwareDataType",
	).Output()
	if err != nil {
		return "", 0
	}

	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "Chip:") {
			name = strings.TrimPrefix(line, "Chip: ")
		}
		if strings.HasPrefix(line, "Memory:") {
			raw := strings.TrimPrefix(line, "Memory: ")
			raw = strings.TrimSuffix(raw, " GB")
			if gb, err := strconv.ParseUint(strings.TrimSpace(raw), 10, 64); err == nil {
				unifiedMemoryMB = gb * 1024
			}
		}
	}
	return name, unifiedMemoryMB
}

// SuggestModels returns an ordered list of models the hardware can run.
func SuggestModels(hw HardwareInfo) []ModelSuggestion {
	// Effective VRAM: if GPU exists use VRAM, otherwise fall back to RAM
	// (CPU-only inference uses RAM instead)
	effectiveMB := hw.TotalRAMMB
	if hw.HasGPU {
		effectiveMB = hw.GPUVRAMMB
	}

	all := []ModelSuggestion{
		{
			Name:          "Llama-3.1-8B-Instruct (Q4_K_M)",
			ID:            "llama3-8b",
			VRAMMBRequired: 5200,
			RAMMBRequired:  5200,
			Quality:       "★★★★☆",
			URL: "https://huggingface.co/bartowski/Meta-Llama-3.1-8B-Instruct-GGUF/resolve/main/Meta-Llama-3.1-8B-Instruct-Q4_K_M.gguf",
		},
		{
			Name:          "Llama-3.2-3B-Instruct (Q4_K_M)",
			ID:            "llama3-3b",
			VRAMMBRequired: 2300,
			RAMMBRequired:  2300,
			Quality:       "★★★☆☆",
			URL: "https://huggingface.co/bartowski/Llama-3.2-3B-Instruct-GGUF/resolve/main/Llama-3.2-3B-Instruct-Q4_K_M.gguf",
		},
		{
			Name:          "Phi-3-mini-4k-Instruct (Q4)",
			ID:            "phi3",
			VRAMMBRequired: 2500,
			RAMMBRequired:  2500,
			Quality:       "★★★☆☆",
			URL: "https://huggingface.co/microsoft/Phi-3-mini-4k-instruct-gguf/resolve/main/Phi-3-mini-4k-instruct-q4.gguf",
		},
		{
			Name:          "Llama-3.1-70B-Instruct (Q4_K_M)",
			ID:            "llama3-70b",
			VRAMMBRequired: 42000,
			RAMMBRequired:  42000,
			Quality:       "★★★★★",
			URL: "https://huggingface.co/bartowski/Meta-Llama-3.1-70B-Instruct-GGUF/resolve/main/Meta-Llama-3.1-70B-Instruct-Q4_K_M.gguf",
		},
	}

	var fits []ModelSuggestion
	for _, m := range all {
		if effectiveMB >= m.VRAMMBRequired {
			fits = append(fits, m)
		}
	}
	return fits
}
