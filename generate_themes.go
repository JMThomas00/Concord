package main

import (
	"fmt"
	"os"
	"path/filepath"
)

type ThemeData struct {
	Name       string
	Author     string
	Variant    string
	Background string
	Foreground string
	Selection  string
	Comment    string
	Red        string
	Orange     string
	Yellow     string
	Green      string
	Cyan       string
	Purple     string
	Pink       string
}

func generateTheme(data ThemeData, filename string) error {
	content := fmt.Sprintf(`[meta]
name = "%s"
author = "%s"
variant = "%s"

[colors]
background = "%s"
current_line = "%s"
selection = "%s"
foreground = "%s"
comment = "%s"
red = "%s"
orange = "%s"
yellow = "%s"
green = "%s"
cyan = "%s"
purple = "%s"
pink = "%s"

[semantic]
sidebar_bg = "%s"
sidebar_fg = "%s"
sidebar_selected = "%s"
sidebar_hover = "%s"
chat_bg = "%s"
chat_fg = "%s"
chat_timestamp = "%s"
chat_username_self = "%s"
chat_username_other = "%s"
chat_mention = "%s"
input_bg = "%s"
input_fg = "%s"
input_border = "%s"
input_border_focus = "%s"
status_online = "%s"
status_idle = "%s"
status_dnd = "%s"
status_offline = "%s"
error = "%s"
warning = "%s"
success = "%s"
info = "%s"
scrollbar = "%s"
scrollbar_hover = "%s"
border = "%s"
`,
		data.Name, data.Author, data.Variant,
		data.Background, lighten(data.Background, 0.1), data.Selection, data.Foreground, data.Comment,
		data.Red, data.Orange, data.Yellow, data.Green, data.Cyan, data.Purple, data.Pink,
		data.Background, data.Foreground, data.Selection, lighten(data.Background, 0.05),
		data.Background, data.Foreground, data.Comment, data.Purple, data.Cyan, data.Pink,
		lighten(data.Background, 0.1), data.Foreground, data.Selection, data.Purple,
		data.Green, data.Yellow, data.Red, data.Comment,
		data.Red, data.Orange, data.Green, data.Cyan,
		data.Selection, data.Comment, data.Selection,
	)

	path := filepath.Join("internal/themes/themes", filename)
	return os.WriteFile(path, []byte(content), 0644)
}

func lighten(hex string, factor float64) string {
	return hex // simplified for now
}

func main() {
	themes := []struct {
		data     ThemeData
		filename string
	}{
		// Add all 40 themes here
	}

	for _, t := range themes {
		if err := generateTheme(t.data, t.filename); err != nil {
			fmt.Printf("Error creating %s: %v\n", t.filename, err)
		} else {
			fmt.Printf("Created %s\n", t.filename)
		}
	}
}
