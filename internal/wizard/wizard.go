// Copyright (C) 2026 caicoders (https://github.com/caicoders)
// SPDX-License-Identifier: AGPL-3.0-or-later

package wizard

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/AlecAivazis/survey/v2"
	"github.com/fatih/color"

	"github.com/caicoders/getares/internal/config"
)

var (
	bold  = color.New(color.Bold)
	cyan  = color.New(color.FgCyan, color.Bold)
	green = color.New(color.FgGreen)
	gray  = color.New(color.FgHiBlack)
	warn  = color.New(color.FgYellow)
	red   = color.New(color.FgRed)
)

// Run executes the interactive init wizard and writes getares.yaml.
func Run() error {
	printHeader()

	// ── Role ─────────────────────────────────────────────────────────────────

	var roleAnswer string
	if err := survey.AskOne(&survey.Select{
		Message: "How will this machine be used?",
		Options: []string{
			"Coordinator — manages the cluster and exposes the OpenAI API",
			"Worker      — runs AI models locally (needs GPU or plenty of RAM)",
			"Both        — coordinator + worker on the same machine (single-machine setup)",
		},
	}, &roleAnswer); err != nil {
		return err
	}

	role := config.RoleCoordinator
	switch {
	case strings.HasPrefix(roleAnswer, "Worker"):
		role = config.RoleWorker
	case strings.HasPrefix(roleAnswer, "Both"):
		role = config.RoleBoth
	}

	cfg := &config.Config{Role: role}

	// ── Coordinator config ────────────────────────────────────────────────────

	if role == config.RoleCoordinator || role == config.RoleBoth {
		if err := askCoordinatorConfig(cfg); err != nil {
			return err
		}
	}

	// ── Worker config ─────────────────────────────────────────────────────────

	if role == config.RoleWorker || role == config.RoleBoth {
		if err := askWorkerConfig(cfg, role); err != nil {
			return err
		}
	}

	// ── Write config ──────────────────────────────────────────────────────────

	if err := config.Save(cfg); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}

	printNextSteps(cfg)
	return nil
}

func askCoordinatorConfig(cfg *config.Config) error {
	cyan.Println("\n── Coordinator settings ───────────────────────────────────")

	var grpcAddr, httpAddr string

	if err := survey.AskOne(&survey.Input{
		Message: "gRPC address for workers:",
		Default: ":9090",
		Help:    "Workers register and send heartbeats here. Use 0.0.0.0:9090 to accept from any interface.",
	}, &grpcAddr); err != nil {
		return err
	}

	if err := survey.AskOne(&survey.Input{
		Message: "HTTP address for the OpenAI-compatible API:",
		Default: ":8080",
		Help:    "Clients (curl, VS Code, etc.) connect here.",
	}, &httpAddr); err != nil {
		return err
	}

	cfg.Coordinator = config.CoordConfig{
		GRPCAddr: grpcAddr,
		HTTPAddr: httpAddr,
	}
	return nil
}

func askWorkerConfig(cfg *config.Config, role string) error {
	cyan.Println("\n── Hardware detection ──────────────────────────────────────")
	fmt.Print("  Scanning hardware...\n")

	hw := DetectHardware()
	printHardwareSummary(hw)

	// Node ID
	defaultID := "worker-1"
	if hostname, err := os.Hostname(); err == nil && hostname != "" {
		defaultID = "worker-" + strings.ToLower(hostname)
	}

	var nodeID string
	if err := survey.AskOne(&survey.Input{
		Message: "Node identifier:",
		Default: defaultID,
		Help:    "Unique name for this worker in the cluster.",
	}, &nodeID); err != nil {
		return err
	}
	cfg.NodeID = nodeID

	// Coordinator address — only relevant for pure worker role
	var coordAddr string
	if role == config.RoleWorker {
		if err := survey.AskOne(&survey.Input{
			Message: "Coordinator address (IP:port):",
			Default: "localhost:9090",
			Help:    "IP and port of the coordinator machine. Example: coordinator.example.com:9090",
		}, &coordAddr); err != nil {
			return err
		}
	} else {
		coordAddr = "localhost:9090"
	}

	// Model
	cyan.Println("\n── Model selection ─────────────────────────────────────────")
	modelPath, modelID, err := askModelSelection(SuggestModels(hw))
	if err != nil {
		return err
	}

	cfg.Worker = config.WorkerConfig{
		Listen:      ":9091",
		LlamaPort:   8081,
		ModelPath:   modelPath,
		ModelID:     modelID,
		Coordinator: coordAddr,
	}
	return nil
}

func askModelSelection(suggestions []ModelSuggestion) (path, id string, err error) {
	var options []string

	if len(suggestions) > 0 {
		green.Println("  Models compatible with your hardware:")
		for i, s := range suggestions {
			label := fmt.Sprintf("%s  %s", s.Quality, s.Name)
			if i == 0 {
				label += "  ← recommended"
			}
			fmt.Printf("    %s\n", label)
			options = append(options, s.Name)
		}
		fmt.Println()
	} else {
		warn.Println("  No compatible GPU found or insufficient memory.")
		warn.Println("  You can still specify a model path manually.")
		fmt.Println()
	}

	options = append(options, "Enter model path manually")

	var modelChoice string
	if err := survey.AskOne(&survey.Select{
		Message: "Choose a model:",
		Options: options,
	}, &modelChoice); err != nil {
		return "", "", err
	}

	if modelChoice == "Enter model path manually" {
		return askManualModelPath()
	}

	for _, s := range suggestions {
		if s.Name == modelChoice {
			defaultDir := defaultModelDir()
			var saveDir string
			if err := survey.AskOne(&survey.Input{
				Message: "Directory to save the model:",
				Default: defaultDir,
			}, &saveDir); err != nil {
				return "", "", err
			}
			modelPath := filepath.Join(saveDir, filepath.Base(s.URL))
			if _, err := os.Stat(modelPath); os.IsNotExist(err) {
				printDownloadInstructions(s, modelPath)
			} else {
				green.Printf("  ✓ Model already exists: %s\n", modelPath)
			}
			return modelPath, s.ID, nil
		}
	}

	return askManualModelPath()
}

func askManualModelPath() (path, id string, err error) {
	var modelPath string
	if err := survey.AskOne(&survey.Input{
		Message: "Full path to your .gguf file:",
		Help:    "Example: /path/to/your/model.gguf  or  C:\\models\\model.gguf",
	}, &modelPath, survey.WithValidator(func(val interface{}) error {
		p := val.(string)
		if home, err := os.UserHomeDir(); err == nil {
			p = strings.Replace(p, "~", home, 1)
		}
		if _, err := os.Stat(p); os.IsNotExist(err) {
			return fmt.Errorf("file not found: %s", p)
		}
		return nil
	})); err != nil {
		return "", "", err
	}

	var modelID string
	if err := survey.AskOne(&survey.Input{
		Message: "Model ID (short alias for routing):",
		Default: "default",
		Help:    "Examples: phi3, llama3, mistral",
	}, &modelID); err != nil {
		return "", "", err
	}

	return modelPath, modelID, nil
}

// ── Output helpers ─────────────────────────────────────────────────────────────

func printHeader() {
	cyan.Println(`
  ╔═══════════════════════════════════════════════╗
  ║   Getares — Distributed AI Runtime           ║
  ║   Interactive setup wizard                   ║
  ╚═══════════════════════════════════════════════╝`)
	gray.Println("  Generates getares.yaml in the current directory.")
	gray.Println("  Run 'getares start' when done.\n")
}

func printHardwareSummary(hw HardwareInfo) {
	fmt.Printf("  OS:   %s/%s\n", hw.OS, runtime.GOARCH)
	fmt.Printf("  RAM:  %d MB (%.0f GB)\n", hw.TotalRAMMB, float64(hw.TotalRAMMB)/1024)
	if hw.HasGPU {
		green.Printf("  GPU:  %s — %d MB VRAM\n", hw.GPUName, hw.GPUVRAMMB)
	} else {
		warn.Println("  GPU:  not detected — CPU-only mode")
		if runtime.GOOS == "windows" || runtime.GOOS == "linux" {
			gray.Println("        (ensure nvidia-smi is on PATH if you have an NVIDIA GPU)")
		}
	}
	fmt.Println()
}

func printDownloadInstructions(s ModelSuggestion, savePath string) {
	dir := filepath.Dir(savePath)
	cyan.Println("\n  ── Download this model ────────────────────────────────────")
	fmt.Printf("  File: %s\n\n", savePath)

	if runtime.GOOS == "windows" {
		bold.Println("  PowerShell:")
		fmt.Printf("    New-Item -ItemType Directory -Force -Path \"%s\"\n", dir)
		fmt.Printf("    curl.exe -L -o \"%s\" \"%s\"\n\n", savePath, s.URL)
	} else {
		bold.Println("  Linux / macOS:")
		fmt.Printf("    mkdir -p %s\n", dir)
		fmt.Printf("    curl -L -o %s \\\n      %s\n\n", savePath, s.URL)
	}
	warn.Println("  Run 'getares start' after the download finishes.")
}

// printNextSteps is the key function for multi-machine clarity.
// It resolves the actual LAN IP and shows topology-specific instructions.
func printNextSteps(cfg *config.Config) {
	lanIP := outboundLANIP()

	green.Println("\n  ✓ Configuration saved to getares.yaml\n")
	cyan.Println("  ── Next steps ─────────────────────────────────────────────")

	switch cfg.Role {

	case config.RoleCoordinator:
		// Extract port from grpc addr (e.g. ":9090" → "9090")
		grpcPort := portFromAddr(cfg.Coordinator.GRPCAddr)
		httpPort := portFromAddr(cfg.Coordinator.HTTPAddr)

		fmt.Println("  1. Start the coordinator on this machine:")
		bold.Println("       getares start")
		fmt.Println()
		fmt.Println("  2. On each worker machine, run:")
		bold.Println("       getares init   # choose: Worker")
		fmt.Printf("                      # coordinator address: %s:%s\n", lanIP, grpcPort)
		fmt.Println()
		fmt.Println("  3. Clients send requests to:")
		bold.Printf("       http://%s:%s/v1/chat/completions\n", lanIP, httpPort)
		fmt.Println()

		if runtime.GOOS == "windows" {
			printWindowsFirewallInstructions(grpcPort, httpPort)
		}

	case config.RoleWorker:
		workerPort := portFromAddr(cfg.Worker.Listen)

		fmt.Println("  1. Make sure the coordinator is running at:", cfg.Worker.Coordinator)
		fmt.Println("  2. Download the model if you haven't yet.")
		fmt.Println("  3. Start this worker:")
		bold.Println("       getares start")
		fmt.Println()

		if runtime.GOOS == "windows" {
			printWindowsFirewallInstructions(workerPort, "")
		}

	case config.RoleBoth:
		httpPort := portFromAddr(cfg.Coordinator.HTTPAddr)

		fmt.Println("  1. Download the model if not done yet (see instructions above).")
		fmt.Println("  2. Start everything on this machine:")
		bold.Println("       getares start")
		fmt.Println()
		fmt.Println("  3. Clients on other machines connect to:")
		bold.Printf("       http://%s:%s/v1/chat/completions\n", lanIP, httpPort)
		fmt.Println()
		fmt.Println("  4. Quick test from any machine on the LAN:")
		bold.Printf("       curl -s http://%s:%s/v1/chat/completions \\\n", lanIP, httpPort)
		bold.Printf("         -H \"Content-Type: application/json\" \\\n")
		bold.Printf("         -d '{\"model\":\"%s\",\"messages\":[{\"role\":\"user\",\"content\":\"Hello\"}],\"stream\":true}'\n",
			cfg.Worker.ModelID)
		fmt.Println()

		if runtime.GOOS == "windows" {
			grpcPort := portFromAddr(cfg.Coordinator.GRPCAddr)
			workerPort := portFromAddr(cfg.Worker.Listen)
			printWindowsFirewallInstructions(grpcPort, httpPort, workerPort)
		}
	}

	fmt.Println()
	gray.Println("  Edit getares.yaml to change settings.")
	gray.Println("  Re-run 'getares init' to start over.\n")
}

// printWindowsFirewallInstructions prints the exact PowerShell commands
// to open the required ports. Only called on Windows.
func printWindowsFirewallInstructions(ports ...string) {
	warn.Println("  ── Windows Firewall ───────────────────────────────────────")
	warn.Println("  Open a PowerShell terminal as Administrator and run:")
	fmt.Println()
	for _, port := range ports {
		if port == "" {
			continue
		}
		fmt.Printf("    New-NetFirewallRule -DisplayName \"Getares :%s\" \\\n", port)
		fmt.Printf("      -Direction Inbound -Protocol TCP -LocalPort %s -Action Allow\n\n", port)
	}
}

// outboundLANIP returns the machine's LAN IP using the UDP routing trick.
func outboundLANIP() string {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "127.0.0.1"
	}
	defer conn.Close()
	return conn.LocalAddr().(*net.UDPAddr).IP.String()
}

// portFromAddr extracts the port number from ":9090" or "0.0.0.0:9090".
func portFromAddr(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return port
}

func defaultModelDir() string {
	home, _ := os.UserHomeDir()
	if runtime.GOOS == "windows" {
		return filepath.Join(home, "getares", "models")
	}
	return filepath.Join(home, ".getares", "models")
}
