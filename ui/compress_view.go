package ui

import (
	"context"
	"fmt"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"peakzip/task"
)

// CompressView provides the UI for the compression workflow.
type CompressView struct {
	app             *App
	container       fyne.CanvasObject
	projectName     *widget.Entry
	sourceDir       *widget.Entry
	outputDir       *widget.Entry
	volumeSize      *widget.Entry
	password        *widget.Entry
	encryptManifest *widget.Check
	progressBar     *widget.ProgressBar
	statusLabel     *widget.Label
	startBtn        *widget.Button
	cancelBtn       *widget.Button
	progressCh      chan task.ProgressUpdate
}

// NewCompressView creates a new compression view.
func NewCompressView(a *App) *CompressView {
	v := &CompressView{
		app:        a,
		progressCh: make(chan task.ProgressUpdate, 100),
	}
	v.buildLayout()
	return v
}

// Container returns the view's container.
func (v *CompressView) Container() fyne.CanvasObject { return v.container }

func (v *CompressView) buildLayout() {
	// Project name entry.
	v.projectName = widget.NewEntry()
	v.projectName.PlaceHolder = "例如：myproject"
	projectRow := container.NewBorder(nil, nil,
		widget.NewLabel("项目名称："), nil, v.projectName,
	)

	// Source directory entry.
	v.sourceDir = widget.NewEntry()
	v.sourceDir.PlaceHolder = "待压缩目录"
	browseSourceBtn := widget.NewButtonWithIcon("浏览", theme.FolderOpenIcon(), func() {
		v.app.SelectFolder("Select Source Directory", func(path string) {
			v.sourceDir.Text = path
		})
	})
	sourceDirRow := container.NewBorder(nil, nil,
		widget.NewLabel("源目录："), browseSourceBtn, v.sourceDir,
	)

	// Output directory entry.
	v.outputDir = widget.NewEntry()
	v.outputDir.PlaceHolder = "卷文件保存位置"
	browseOutputBtn := widget.NewButtonWithIcon("浏览", theme.FolderOpenIcon(), func() {
		v.app.SelectFolder("Select Output Directory", func(path string) {
			v.outputDir.Text = path
		})
	})
	outputDirRow := container.NewBorder(nil, nil,
		widget.NewLabel("输出目录："), browseOutputBtn, v.outputDir,
	)

	// Volume size entry.
	v.volumeSize = widget.NewEntry()
	v.volumeSize.Text = strconv.FormatInt(v.app.Settings().DefaultVolumeSize/1024/1024, 10)
	volSizeRow := container.NewBorder(nil, nil,
		widget.NewLabel("分卷大小 (MB)："),
		widget.NewButtonWithIcon("100", theme.WindowMaximizeIcon(), func() {
			v.volumeSize.Text = "100"
		}),
		v.volumeSize,
	)

	// Password entry.
	v.password = widget.NewPasswordEntry()
	v.password.PlaceHolder = "留空则不加密"
	passRow := container.NewBorder(nil, nil,
		widget.NewLabel("密码："),
		widget.NewButtonWithIcon("", theme.InfoIcon(), func() {}),
		v.password,
	)

	// Encrypt manifest checkbox.
	v.encryptManifest = widget.NewCheck("加密清单卷", func(bool) {})
	encryptManifestRow := container.NewBorder(nil, nil, nil, nil, v.encryptManifest)

	// Progress bar and status label.
	v.progressBar = widget.NewProgressBar()
	v.progressBar.Hide()
	v.statusLabel = widget.NewLabel("")

	// Action buttons.
	v.startBtn = widget.NewButtonWithIcon("开始压缩", theme.MediaPlayIcon(), v.startCompression)
	v.startBtn.Importance = widget.HighImportance
	v.cancelBtn = widget.NewButtonWithIcon("取消", theme.CancelIcon(), func() {
		v.app.Cancel()
	})
	v.cancelBtn.Hide()
	actionRow := container.NewHBox(v.startBtn, v.cancelBtn)

	// Layout.
	v.container = container.NewBorder(
		nil,
		container.NewVBox(widget.NewSeparator(), actionRow),
		nil, nil,
		container.NewVBox(
			widget.NewLabel("压缩设置"),
			widget.NewSeparator(),
			projectRow,
			widget.NewSeparator(),
			sourceDirRow,
			widget.NewSeparator(),
			outputDirRow,
			widget.NewSeparator(),
			volSizeRow,
			widget.NewSeparator(),
			passRow,
			encryptManifestRow,
			widget.NewSeparator(),
			widget.NewLabel("进度"),
			v.progressBar,
			v.statusLabel,
		),
	)
}

func (v *CompressView) startCompression() {
	v.app.Logger().Debug(fmt.Sprintf("开始压缩任务：项目=%s，源目录=%s，输出目录=%s", v.projectName.Text, v.sourceDir.Text, v.outputDir.Text))
	if v.projectName.Text == "" {
		v.app.ShowError(fmt.Errorf("项目名称不能为空"))
		return
	}
	if v.sourceDir.Text == "" {
		v.app.ShowError(fmt.Errorf("源目录不能为空"))
		return
	}
	if v.outputDir.Text == "" {
		v.app.ShowError(fmt.Errorf("输出目录不能为空"))
		return
	}

	volumeSizeMB, err := strconv.ParseInt(v.volumeSize.Text, 10, 64)
	if err != nil || volumeSizeMB <= 0 {
		v.app.ShowError(fmt.Errorf("无效的分卷大小"))
		return
	}
	volumeSize := volumeSizeMB * 1024 * 1024

	// Disable start button, enable cancel.
	v.startBtn.Disable()
	v.cancelBtn.Show()
	v.progressBar.Show()
	v.progressBar.SetValue(0)

	cfg := task.CompressTask{
		ProjectName:     v.projectName.Text,
		SourceDir:       v.sourceDir.Text,
		OutputDir:       v.outputDir.Text,
		VolumeSize:      volumeSize,
		Password:        v.password.Text,
		EncryptManifest: v.encryptManifest.Checked,
	}

	// Run compression in goroutine.
	ctx, cancel := context.WithCancel(v.app.Context())
	defer cancel()

	go func() {
		err := task.RunCompress(ctx, cfg, v.progressCh, v.app.Logger())
		if err != nil {
			if ctx.Err() != nil {
				v.app.Logger().Info("压缩已取消")
			} else {
				v.app.Logger().Error(fmt.Sprintf("压缩失败：%v", err))
				v.app.ShowError(err)
			}
		}
		v.startBtn.Enable()
		v.cancelBtn.Hide()
	}()

	// Listen for progress updates on the Fyne thread.
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
