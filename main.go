// PeakZip - Low Peak Volume Compression/Extraction Management Tool
//
// A GUI tool for compressing large projects into logical volumes and
// extracting them with minimal peak disk space usage.
//
// Usage:
//
//	peakzip.exe              正常模式（仅写入 ~/.peakzip/peakzip.log）
//	peakzip.exe -debug       调试模式（额外写入 peakzip_debug.log 到程序当前目录）
//	peakzip.exe -h           显示帮助
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"peakzip/ui"
)

func main() {
	debugMode := flag.Bool("debug", false, "启用调试模式：在程序当前目录生成 peakzip_debug.log")
	flag.Parse()

	// Determine log file path (in user home).
	logPath := ""
	if home, err := os.UserHomeDir(); err == nil {
		logDir := filepath.Join(home, ".peakzip")
		if err := os.MkdirAll(logDir, 0o755); err == nil {
			logPath = filepath.Join(logDir, "peakzip.log")
		}
	}

	// Determine debug log path (in current working directory).
	debugPath := ""
	if *debugMode {
		if wd, err := os.Getwd(); err == nil {
			debugPath = filepath.Join(wd, "peakzip_debug.log")
		}
	}

	// Create the application.
	app, err := ui.NewApp(logPath, debugPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "初始化应用程序失败：%v\n", err)
		os.Exit(1)
	}

	// Run the application (blocks until main window is closed).
	app.Run()

	// Clean up.
	app.Logger().Close()
}
