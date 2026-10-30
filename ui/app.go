// Package ui implements the Fyne v2 graphical interface for peakzip.
package ui

import (
	"context"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"peakzip/logger"
)

// Settings holds user-configurable application settings.
type Settings struct {
	mu                   sync.RWMutex
	SevenZipPath         string
	LogLevel             logger.Level
	DefaultVolumeSize    int64
	MaxConcurrentWorkers int
	DeleteAfterExtract   bool
}

// DefaultSettings returns a Settings with sensible defaults.
func DefaultSettings() *Settings {
	return &Settings{
		SevenZipPath:         "",
		LogLevel:             logger.LevelInfo,
		DefaultVolumeSize:    100 * 1024 * 1024,
		MaxConcurrentWorkers: 4,
		DeleteAfterExtract:   false,
	}
}

// App is the main application structure holding all windows and state.
type App struct {
	fyneApp  fyne.App
	mainWnd  fyne.Window
	settings *Settings
	logger   *logger.Logger

	compressView   *CompressView
	decompressView *DecompressView
	logWnd         *LogWindow
	manifestView   *ManifestView
	settingsPanel  *SettingsPanel

	ctx    context.Context
	cancel context.CancelFunc
}

// NewApp creates a new peakzip application with the given log paths.
// logPath is the main log file (in user home), debugPath is the debug log file
// (in the program's current directory). If debugPath is empty, debug logging is disabled.
func NewApp(logPath, debugPath string) (*App, error) {
	a := &App{
		fyneApp:  app.NewWithID("com.peakzip.app"),
		settings: DefaultSettings(),
	}

	var err error
	a.logger, err = logger.New(logPath, logger.LevelDebug)
	if err != nil {
		return nil, err
	}
	if debugPath != "" {
		a.logger.AddFile(debugPath)
	} else {
		// Normal mode: suppress debug messages in the main log.
		a.logger.SetLevel(logger.LevelInfo)
	}

	a.ctx, a.cancel = context.WithCancel(context.Background())

	a.mainWnd = a.fyneApp.NewWindow("峰压 - 低峰值分卷压缩解压管理工具")
	a.mainWnd.Resize(fyne.NewSize(900, 650))

	a.compressView = NewCompressView(a)
	a.decompressView = NewDecompressView(a)
	a.logWnd = NewLogWindow(a)
	a.manifestView = NewManifestView(a)
	a.settingsPanel = NewSettingsPanel(a)

	a.buildMainWindow()

	a.logger.Debug("应用程序初始化完成")
	return a, nil
}

// Run starts the application and blocks until the main window is closed.
func (a *App) Run() {
	a.mainWnd.ShowAndRun()
}

// Logger returns the application logger.
func (a *App) Logger() *logger.Logger { return a.logger }

// Settings returns the application settings.
func (a *App) Settings() *Settings { return a.settings }

// FyneApp returns the underlying Fyne app instance.
func (a *App) FyneApp() fyne.App { return a.fyneApp }

// MainWindow returns the main window.
func (a *App) MainWindow() fyne.Window { return a.mainWnd }

// Cancel stops the current task.
func (a *App) Cancel() {
	a.cancel()
	a.ctx, a.cancel = context.WithCancel(context.Background())
}

// Context returns the current cancellation context.
func (a *App) Context() context.Context { return a.ctx }

// buildMainWindow creates the main window layout.
func (a *App) buildMainWindow() {
	a.showView(a.compressView.Container())
}

// showView switches the main window content to the given view content.
func (a *App) showView(content fyne.CanvasObject) {
	toolbar := container.NewHBox(
		widget.NewButtonWithIcon("压缩", theme.DocumentCreateIcon(), func() {
			a.logger.Debug("切换到压缩视图")
			a.showView(a.compressView.Container())
		}),
		widget.NewButtonWithIcon("解压", theme.ContentRemoveIcon(), func() {
			a.logger.Debug("切换到解压视图")
			a.showView(a.decompressView.Container())
		}),
		widget.NewSeparator(),
		widget.NewButtonWithIcon("日志", theme.InfoIcon(), func() {
			a.logger.Debug("打开日志窗口")
			a.logWnd.Show()
		}),
		widget.NewButtonWithIcon("设置", theme.SettingsIcon(), func() {
			a.logger.Debug("打开设置窗口")
			a.settingsPanel.Show()
		}),
		widget.NewButtonWithIcon("清单", theme.DocumentIcon(), func() {
			a.logger.Debug("打开清单窗口")
			a.manifestView.Show()
		}),
	)

	a.mainWnd.SetContent(container.NewBorder(toolbar, nil, nil, nil, content))
}

// ShowError displays an error dialog on the main window.
func (a *App) ShowError(err error) {
	dialog.ShowError(err, a.mainWnd)
}

// SelectFolder shows a folder picker dialog.
func (a *App) SelectFolder(title string, onSelected func(string)) {
	dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
		if err != nil {
			a.ShowError(err)
			return
		}
		onSelected(uri.Path())
	}, a.mainWnd).Show()
}

// SelectFile shows a native file picker dialog.
//
// title and extensions are kept so callers stay unchanged; Fyne does not expose
// a title or extension filter hook on NewFileOpen.
func (a *App) SelectFile(title string, extensions []string, onSelected func(string)) {
	dialog.NewFileOpen(func(uri fyne.URIReadCloser, err error) {
		if err != nil {
			a.ShowError(err)
			return
		}
		defer uri.Close()
		onSelected(uri.URI().Path())
	}, a.mainWnd).Show()
}
