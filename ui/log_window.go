package ui

import (
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"peakzip/logger"
)

// LogWindow is a secondary window for viewing log messages.
type LogWindow struct {
	app        *App
	wnd        fyne.Window
	entries    []logger.Entry
	mu         sync.Mutex
	list       *widget.List
	filter     *widget.Select
	closed     bool // window was closed by user (X button)
	subscribed bool // logger subscription started
}

// NewLogWindow creates a new log viewer window.
func NewLogWindow(a *App) *LogWindow {
	l := &LogWindow{
		app:     a,
		entries: make([]logger.Entry, 0),
	}
	l.buildWindow()
	l.subscribeLogger()
	return l
}

func (l *LogWindow) buildWindow() {
	l.wnd = l.app.FyneApp().NewWindow("峰压 - 日志查看器")
	l.wnd.Resize(fyne.NewSize(700, 500))
	l.wnd.SetOnClosed(func() {
		l.closed = true
		l.app.Logger().Debug("日志窗口被用户关闭")
	})

	// Filter dropdown.
	l.filter = widget.NewSelect([]string{"全部", "DEBUG", "INFO", "WARNING", "ERROR"}, func(string) {
		l.refreshList()
	})
	l.filter.Selected = "全部"

	filterRow := container.NewBorder(nil, nil,
		widget.NewLabel("筛选："),
		widget.NewButtonWithIcon("清空", theme.CancelIcon(), l.clear),
		l.filter,
	)

	// Log list.
	l.list = widget.NewList(
		func() int { return len(l.filteredEntries()) },
		func() fyne.CanvasObject {
			return container.NewBorder(
				nil, nil,
				widget.NewLabel(""), // time
				widget.NewLabel(""), // level
				widget.NewLabel(""), // message
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			entries := l.filteredEntries()
			if id >= len(entries) {
				return
			}
			e := entries[id]
			entryObj := obj.(*fyne.Container)
			entryObj.Objects[0].(*widget.Label).SetText(e.Time.Format("15:04:05"))
			entryObj.Objects[1].(*widget.Label).SetText(e.Level.String())
			entryObj.Objects[2].(*widget.Label).SetText(e.Message)
		},
	)

	l.wnd.SetContent(container.NewBorder(filterRow, nil, nil, nil, l.list))

	l.app.Logger().Debug("日志窗口已构建")
}

// Subscribe to the logger so log entries are collected regardless of window state.
func (l *LogWindow) subscribeLogger() {
	if l.subscribed {
		return
	}
	l.subscribed = true
	go func() {
		ch := l.app.Logger().Subscribe()
		for entry := range ch {
			l.mu.Lock()
			l.entries = append(l.entries, entry)
			if len(l.entries) > 1000 {
				l.entries = l.entries[len(l.entries)-1000:]
			}
			l.mu.Unlock()
			// Refresh only if list exists (window may have been closed)
			if l.list != nil {
				l.list.Refresh()
			}
		}
	}()
}

// Show displays the log window. Rebuilds if the window was closed by the user.
func (l *LogWindow) Show() {
	if l.closed {
		l.app.Logger().Debug("日志窗口重建中...")
		l.buildWindow()
		l.closed = false
	}
	l.wnd.Show()
}

// Close hides the log window.
func (l *LogWindow) Close() {
	l.wnd.Hide()
}

// clear removes all log entries.
func (l *LogWindow) clear() {
	l.mu.Lock()
	l.entries = nil
	l.mu.Unlock()
	l.refreshList()
}

// filteredEntries returns entries filtered by the current filter setting.
func (l *LogWindow) filteredEntries() []logger.Entry {
	l.mu.Lock()
	defer l.mu.Unlock()

	switch l.filter.Selected {
	case "DEBUG":
		var result []logger.Entry
		for _, e := range l.entries {
			if e.Level == logger.LevelDebug {
				result = append(result, e)
			}
		}
		return result
	case "INFO":
		var result []logger.Entry
		for _, e := range l.entries {
			if e.Level == logger.LevelInfo {
				result = append(result, e)
			}
		}
		return result
	case "WARNING":
		var result []logger.Entry
		for _, e := range l.entries {
			if e.Level == logger.LevelWarning {
				result = append(result, e)
			}
		}
		return result
	case "ERROR":
		var result []logger.Entry
		for _, e := range l.entries {
			if e.Level == logger.LevelError {
				result = append(result, e)
			}
		}
		return result
	default:
		return l.entries
	}
}

// refreshList triggers a list refresh.
func (l *LogWindow) refreshList() {
	if l.list != nil {
		l.list.Refresh()
	}
}
