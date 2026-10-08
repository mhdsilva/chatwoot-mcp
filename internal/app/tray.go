package app

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/gogpu/systray"
)

//go:embed icon.png
var iconPNG []byte

// runTray shows the menu bar / notification area icon. A panic from the native
// backend is converted into an error so Run can fall back to a headless run.
func runTray(ctx context.Context, panelURL string, openPanel func()) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("tray backend failed: %v", recovered)
		}
	}()

	tray := systray.New()
	menu := systray.NewMenu()
	menu.Add("Abrir painel", openPanel)
	menu.AddSeparator()
	menu.Add("Sair", func() { tray.Remove() })

	tray.SetIcon(iconPNG).
		SetTooltip("Chatwoot MCP — " + panelURL).
		SetMenu(menu).
		OnClick(openPanel)

	tray.Show()
	go func() {
		<-ctx.Done()
		tray.Remove()
	}()

	if err := tray.Run(); err != nil {
		return fmt.Errorf("tray run: %w", err)
	}
	return nil
}
