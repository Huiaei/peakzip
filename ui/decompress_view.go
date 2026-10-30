package ui

import (
	"context"
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"peakzip/task"
)

// DecompressView provides the UI for the decompression workflow.
type DecompressView struct {
	app            *App
	container      fyne.CanvasObject
	manifestPath   *widget.Entry
	downloadDir    *widget.Entry
	extractDir     *widget.Entry
	password       *widget.Entry
	deleteWhenDone *widget.Check
	progressBar    *widget.ProgressBar
	statusLabel    *widget.Label
	startBtn       *widget.Button
	cancelBtn      *widget.Button
	rescanBtn      *widget.Button
	progressCh     chan task.ProgressUpdate
}

// NewDecompressView creates a new decompression view.
func NewDecompressView(a *App) *DecompressView {
	v := &DecompressView{
		app:        a,
		progressCh: make(chan task.ProgressUpdate, 100),
	}
	v.buildLayout()
	return v
}

// Container returns the view's container.
func (v *DecompressView) Container() fyne.CanvasObject { return v.container }

func (v *DecompressView) buildLayout() {
	// Manifest path entry.
	v.manifestPath = widget.NewEntry()
	v.manifestPath.PlaceHolder = "清单 JSON 文件或 .7z 清单卷"
	browseManifestBtn := widget.NewButtonWithIcon("浏览", theme.FolderOpenIcon(), func() {
		v.app.SelectFile("Select Manifest", []string{".json", ".7z"}, func(path string) {
			v.manifestPath.Text = path
		})
	})
	manifestRow := container.NewBorder(nil, nil,
		widget.NewLabel("清单文件："), browseManifestBtn, v.manifestPath,
	)

	// Download directory entry.
	v.downloadDir = widget.NewEntry()
	v.downloadDir.PlaceHolder = "卷文件下载位置"
	browseDownloadBtn := widget.NewButtonWithIcon("浏览", theme.FolderOpenIcon(), func() {
		v.app.SelectFolder("Select Download Directory", func(path string) {
			v.downloadDir.Text = path
		})
	})
	downloadRow := container.NewBorder(nil, nil,
		widget.NewLabel("下载目录："), browseDownloadBtn, v.downloadDir,
	)

	// Extract directory entry.
	v.extractDir = widget.NewEntry()
	v.extractDir.PlaceHolder = "文件解压位置"
	browseExtractBtn := widget.NewButtonWithIcon("浏览", theme.FolderOpenIcon(), func() {
		v.app.SelectFolder("Select Extract Directory", func(path string) {
			v.extractDir.Text = path
		})
	})
	extractRow := container.NewBorder(nil, nil,
		widget.NewLabel("解压目录："), browseExtractBtn, v.extractDir,
	)

	// Password entry.
	v.password = widget.NewPasswordEntry()
	v.password.PlaceHolder = "如有密码"
	passRow := container.NewBorder(nil, nil,
		widget.NewLabel("密码："), nil, v.password,
	)

	// Delete when done checkbox.
	v.deleteWhenDone = widget.NewCheck("解压完成后删除卷文件", func(bool) {})
	deleteRow := container.NewBorder(nil, nil, nil, nil, v.deleteWhenDone)

	// Progress bar and status label.
	v.progressBar = widget.NewProgressBar()
	v.progressBar.Hide()
	v.statusLabel = widget.NewLabel("")

	// Action buttons.
	v.startBtn = widget.NewButtonWithIcon("开始解压", theme.MediaPlayIcon(), v.startDecompression)
	v.startBtn.Importance = widget.HighImportance
	v.cancelBtn = widget.NewButtonWithIcon("取消", theme.CancelIcon(), func() {
		v.app.Cancel()
	})
	v.cancelBtn.Hide()
	v.rescanBtn = widget.NewButtonWithIcon("重新扫描", theme.SearchIcon(), v.rescanVolumes)
	v.rescanBtn.Hide()
	actionRow := container.NewHBox(v.startBtn, v.cancelBtn, v.rescanBtn)

	// Layout.
	v.container = container.NewBorder(
		nil,
		container.NewVBox(widget.NewSeparator(), actionRow),
		nil, nil,
		container.NewVBox(
			widget.NewLabel("解压设置"),
			widget.NewSeparator(),
			manifestRow,
			widget.NewSeparator(),
			downloadRow,
			widget.NewSeparator(),
			extractRow,
			widget.NewSeparator(),
			passRow,
			deleteRow,
			widget.NewSeparator(),
			widget.NewLabel("进度"),
			v.progressBar,
			v.statusLabel,
		),
	)
}

func (v *DecompressView) startDecompression() {
	v.app.Logger().Debug(fmt.Sprintf("开始解压任务：下载目录=%s，解压目录=%s", v.downloadDir.Text, v.extractDir.Text))
	if v.manifestPath.Text == "" {
		v.app.ShowError(fmt.Errorf("manifest path is required"))
		return
	}
	if v.downloadDir.Text == "" {
		v.app.ShowError(fmt.Errorf("下载目录不能为空"))
		return
	}
	if v.extractDir.Text == "" {
		v.app.ShowError(fmt.Errorf("解压目录不能为空"))
		return
	}

	v.startBtn.Disable()
	v.cancelBtn.Show()
	v.progressBar.Show()
	v.progressBar.SetValue(0)

	cfg := task.DecompressTask{
		ManifestPath:   v.manifestPath.Text,
		DownloadDir:    v.downloadDir.Text,
		ExtractDir:     v.extractDir.Text,
		Password:       v.password.Text,
		DeleteWhenDone: v.deleteWhenDone.Checked,
		ResourceConfig: task.ResourceConfig{
			MaxWorkers:       v.app.Settings().MaxConcurrentWorkers,
			DeleteIntervalMS: 1000,
			ScanIntervalMS:   10000,
			StableWindowSec:  30,
		},
	}

	ctx, cancel := context.WithCancel(v.app.Context())
	defer cancel()

	go func() {
		err := task.RunDecompress(ctx, cfg, v.progressCh, v.app.Logger())
		if err != nil {
			if ctx.Err() != nil {
				v.app.Logger().Info("解压已取消")
			} else {
				v.app.Logger().Error(fmt.Sprintf("解压失败：%v", err))
				v.app.ShowError(err)
			}
		}
		v.startBtn.Enable()
		v.cancelBtn.Hide()
		v.rescanBtn.Hide()
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case update := <-v.progressCh:
				v.progressBar.SetValue(update.Progress)
				v.statusLabel.Text = fmt.Sprintf("[%s] %s (%d/%d)",
					update.Phase, update.Message, update.CurrentVolume, update.TotalVolumes)
				v.app.Logger().Debug(fmt.Sprintf("进度：%s - %s", update.Phase, update.Message))
			}
		}
	}()
}

func (v *DecompressView) rescanVolumes() {
	v.app.Logger().Debug("触发手动重新扫描")
	v.statusLabel.Text = "正在重新扫描卷文件..."
	v.app.Logger().Info("已触发手动重新扫描")
}
