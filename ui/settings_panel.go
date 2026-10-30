package ui

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"peakzip/archiver"
	"peakzip/logger"
)

// SettingsPanel is a secondary window for configuring application settings.
type SettingsPanel struct {
	app            *App
	wnd            fyne.Window
	sevenZipPath   *widget.Entry
	sevenZipStatus *widget.Label
	closed         bool // window was closed by user (X button)
}

// NewSettingsPanel creates a new settings panel window.
func NewSettingsPanel(a *App) *SettingsPanel {
	s := &SettingsPanel{app: a}
	s.buildWindow()
	return s
}

func (s *SettingsPanel) buildWindow() {
	s.wnd = s.app.FyneApp().NewWindow("峰压 - 设置")
	s.wnd.Resize(fyne.NewSize(500, 500))
	s.wnd.SetOnClosed(func() {
		s.closed = true
		s.app.Logger().Debug("设置窗口被用户关闭")
	})

	// 7z path section.
	s.sevenZipPath = widget.NewEntry()
	s.sevenZipPath.Text = s.app.Settings().SevenZipPath
	s.sevenZipStatus = widget.NewLabel("")

	browseSevenZipBtn := widget.NewButtonWithIcon("浏览", theme.FolderOpenIcon(), func() {
		s.app.SelectFile("选择 7z 可执行文件", nil, func(path string) {
			s.sevenZipPath.Text = path
		})
	})

	detectBtn := widget.NewButtonWithIcon("自动检测", theme.SearchIcon(), func() {
		path := archiver.FindSevenZip()
		s.sevenZipPath.Text = path
		if out, err := exec.Command(path, "--help").CombinedOutput(); err == nil {
			line := strings.Split(string(out), "\n")[0]
			s.sevenZipStatus.SetText(fmt.Sprintf("已找到：%s (%s)", path, line))
		} else {
			s.sevenZipStatus.SetText("7z 可执行文件已找到但无法运行")
		}
	})

	sevenZipPathRow := container.NewBorder(nil, nil,
		widget.NewLabel("7z 路径："), browseSevenZipBtn, s.sevenZipPath,
	)

	sevenZipSection := container.NewVBox(
		widget.NewLabel("7-Zip 配置"),
		widget.NewSeparator(),
		sevenZipPathRow,
		container.NewHBox(detectBtn, s.sevenZipStatus),
	)

	// Log level section.
	logLevels := []string{"DEBUG", "INFO", "WARNING", "ERROR"}
	logLevelSelect := widget.NewSelect(logLevels, func(val string) {
		var level logger.Level
		switch val {
		case "DEBUG":
			level = logger.LevelDebug
		case "INFO":
			level = logger.LevelInfo
		case "WARNING":
			level = logger.LevelWarning
		case "ERROR":
			level = logger.LevelError
		}
		s.app.Logger().SetLevel(level)
		s.app.Settings().LogLevel = level
		s.app.Logger().Debug(fmt.Sprintf("日志级别变更为：%s", val))
	})
	logLevelSelect.Selected = s.app.Settings().LogLevel.String()

	logSection := container.NewBorder(nil, nil,
		widget.NewLabel("日志级别："), nil, logLevelSelect,
	)

	// Volume size section.
	volumeSizeEntry := widget.NewEntry()
	volumeSizeEntry.Text = strconv.FormatInt(s.app.Settings().DefaultVolumeSize/1024/1024, 10)
	volumeSizeEntry.OnChanged = func(val string) {
		if n, err := strconv.ParseInt(val, 10, 64); err == nil && n > 0 {
			s.app.Settings().DefaultVolumeSize = n * 1024 * 1024
		}
	}

	volumeSizeSection := container.NewBorder(nil, nil,
		widget.NewLabel("默认分卷大小 (MB)："),
		widget.NewLabel("推荐：100-500"),
		volumeSizeEntry,
	)

	// Worker count section.
	maxWorkersEntry := widget.NewEntry()
	maxWorkersEntry.Text = strconv.Itoa(s.app.Settings().MaxConcurrentWorkers)
	maxWorkersEntry.OnChanged = func(val string) {
		if n, err := strconv.Atoi(val); err == nil && n > 0 && n <= 32 {
			s.app.Settings().MaxConcurrentWorkers = n
		}
	}

	workerSection := container.NewBorder(nil, nil,
		widget.NewLabel("并发数："),
		widget.NewLabel("1-32"),
		maxWorkersEntry,
	)

	// Delete after extract.
	deleteCheck := widget.NewCheck("解压成功后自动删除卷文件", func(checked bool) {
		s.app.Settings().DeleteAfterExtract = checked
	})
	deleteCheck.Checked = s.app.Settings().DeleteAfterExtract

	deleteSection := container.NewBorder(nil, nil, nil, nil, deleteCheck)

	// Save button.
	saveBtn := widget.NewButton("保存设置", func() {
		if s.sevenZipPath.Text != "" {
			s.app.Settings().SevenZipPath = s.sevenZipPath.Text
		}
		s.app.Logger().Info("设置已保存")
		s.app.Logger().Debug(fmt.Sprintf("当前 7z 路径：%s", s.app.Settings().SevenZipPath))
		s.wnd.Hide()
	})

	// Layout.
	s.wnd.SetContent(container.NewBorder(
		nil,
		container.NewVBox(widget.NewSeparator(), saveBtn),
		nil, nil,
		container.NewVBox(
			widget.NewLabel("应用设置"),
			widget.NewSeparator(),
			sevenZipSection,
			widget.NewSeparator(),
			logSection,
			widget.NewSeparator(),
			volumeSizeSection,
			widget.NewSeparator(),
			workerSection,
			widget.NewSeparator(),
			deleteSection,
		),
	))

	s.app.Logger().Debug("设置窗口已构建")
}

// Show displays the settings panel. Rebuilds if the window was closed by the user.
func (s *SettingsPanel) Show() {
	if s.closed {
		s.app.Logger().Debug("设置窗口重建中...")
		s.buildWindow()
		s.closed = false
	}
	s.wnd.Show()
}

// Close hides the settings panel.
func (s *SettingsPanel) Close() {
	s.wnd.Hide()
}
