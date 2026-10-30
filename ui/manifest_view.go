package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"peakzip/manifest"
)

// ManifestView is a secondary window for viewing manifest contents.
type ManifestView struct {
	app          *App
	wnd          fyne.Window
	projectName  *widget.Label
	totalVolumes *widget.Label
	fileCount    *widget.Label
	volumeSize   *widget.Label
	encryption   *widget.Label
	filesList    *widget.List
	volumesList  *widget.List
	mu           sync.Mutex
	m            *manifest.Manifest
	closed       bool // window was closed by user (X button)
}

// NewManifestView creates a new manifest viewer window.
func NewManifestView(a *App) *ManifestView {
	v := &ManifestView{app: a}
	v.buildWindow()
	return v
}

func (v *ManifestView) buildWindow() {
	v.wnd = v.app.FyneApp().NewWindow("峰压 - 清单查看器")
	v.wnd.Resize(fyne.NewSize(800, 600))
	v.wnd.SetOnClosed(func() {
		v.closed = true
		v.app.Logger().Debug("清单查看器窗口被用户关闭")
	})

	// Summary labels.
	v.projectName = widget.NewLabel("")
	v.totalVolumes = widget.NewLabel("")
	v.fileCount = widget.NewLabel("")
	v.volumeSize = widget.NewLabel("")
	v.encryption = widget.NewLabel("")

	summaryGrid := container.NewGridWithColumns(2,
		widget.NewLabel("项目名称："), v.projectName,
		widget.NewLabel("总卷数："), v.totalVolumes,
		widget.NewLabel("文件数："), v.fileCount,
		widget.NewLabel("分卷大小限制："), v.volumeSize,
		widget.NewLabel("加密："), v.encryption,
	)

	// Load button.
	loadBtn := widget.NewButtonWithIcon("加载清单", theme.DocumentIcon(), func() {
		dialog.NewFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				v.app.ShowError(err)
				return
			}
			path := uri.Path()
			info, err := os.Stat(path)
			if err == nil && info.IsDir() {
				entries, err := os.ReadDir(path)
				if err == nil {
					for _, e := range entries {
						if strings.HasSuffix(e.Name(), ".json") {
							path = filepath.Join(path, e.Name())
							break
						}
					}
				}
			}
			v.loadManifest(path)
		}, v.wnd).Show()
	})

	// File list.
	filesTitle := widget.NewLabel("文件列表")
	v.filesList = widget.NewList(
		func() int {
			v.mu.Lock()
			defer v.mu.Unlock()
			if v.m == nil {
				return 0
			}
			return len(v.m.Files)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel(""), widget.NewLabel(""),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			v.mu.Lock()
			m := v.m
			v.mu.Unlock()
			if m == nil || id >= len(m.Files) {
				return
			}
			f := m.Files[id]
			row := obj.(*fyne.Container)
			row.Objects[0].(*widget.Label).SetText(f.Path)
			row.Objects[1].(*widget.Label).SetText(formatSize(f.Size))
		},
	)

	// Volume list.
	volumesTitle := widget.NewLabel("卷列表")
	v.volumesList = widget.NewList(
		func() int {
			v.mu.Lock()
			defer v.mu.Unlock()
			if v.m == nil {
				return 0
			}
			return len(v.m.Volumes)
		},
		func() fyne.CanvasObject {
			return container.NewHBox(
				widget.NewLabel(""), widget.NewLabel(""), widget.NewLabel(""),
			)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			v.mu.Lock()
			m := v.m
			v.mu.Unlock()
			if m == nil || id >= len(m.Volumes) {
				return
			}
			vol := m.Volumes[id]
			row := obj.(*fyne.Container)
			row.Objects[0].(*widget.Label).SetText(fmt.Sprintf("%03d", vol.LogicalNumber))
			row.Objects[1].(*widget.Label).SetText(vol.Filename)
			row.Objects[2].(*widget.Label).SetText(vol.Type)
		},
	)

	filesColumn := container.NewVBox(filesTitle, v.filesList)
	volumesColumn := container.NewVBox(volumesTitle, v.volumesList)

	// Layout.
	v.wnd.SetContent(container.NewBorder(
		container.NewVBox(summaryGrid, widget.NewSeparator(), loadBtn),
		nil,
		filesColumn,
		volumesColumn,
	))

	v.app.Logger().Debug("清单查看器窗口已构建")
}

// Show displays the manifest viewer window. Rebuilds if the window was closed by the user.
func (v *ManifestView) Show() {
	if v.closed {
		v.app.Logger().Debug("清单查看器窗口重建中...")
		v.buildWindow()
		v.closed = false
	}
	v.wnd.Show()
}

// Close hides the manifest viewer.
func (v *ManifestView) Close() {
	v.wnd.Hide()
}

func (v *ManifestView) loadManifest(path string) {
	v.app.Logger().Debug(fmt.Sprintf("正在加载清单：%s", path))
	m, err := manifest.Load(path)
	if err != nil {
		v.app.Logger().Error(fmt.Sprintf("加载清单失败：%v", err))
		v.app.ShowError(fmt.Errorf("load manifest: %w", err))
		return
	}

	v.mu.Lock()
	v.m = m
	v.mu.Unlock()

	v.projectName.SetText(m.ProjectName)
	v.totalVolumes.SetText(fmt.Sprintf("%d", m.TotalVolumes))
	v.fileCount.SetText(fmt.Sprintf("%d", len(m.Files)))
	v.volumeSize.SetText(formatSize(m.VolumeSizeLimit))
	v.encryption.SetText(fmt.Sprintf("%v（算法：%s）",
		m.Encryption.Enabled, m.Encryption.Method))

	v.filesList.Refresh()
	v.volumesList.Refresh()

	v.app.Logger().Info(fmt.Sprintf("清单已加载：%s（%d 个卷，%d 个文件）",
		m.ProjectName, m.TotalVolumes, len(m.Files)))
}

func formatSize(bytes int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case bytes >= GB:
		return fmt.Sprintf("%.1f GB", float64(bytes)/GB)
	case bytes >= MB:
		return fmt.Sprintf("%.1f MB", float64(bytes)/MB)
	case bytes >= KB:
		return fmt.Sprintf("%.1f KB", float64(bytes)/KB)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
