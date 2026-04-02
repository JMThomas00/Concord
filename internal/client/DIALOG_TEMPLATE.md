# Dialog & Modal Template Guide

This guide defines standard patterns for dialogs, modals, and pop-ups in Concord to ensure consistent UX.

---

## Dialog Types

### 1. **Confirmation Dialog** (Yes/No, Accept/Decline)
**Use for**: Destructive actions, irreversible changes, important decisions
**Examples**: Delete role, delete channel, prune messages

**Pattern**:
- Centered dialog overlay
- Warning icon + title (⚠ Confirm Deletion)
- Warning message explaining consequences
- Two buttons side-by-side
- Help text showing keyboard shortcuts

**Navigation**:
- `Tab` or `←→` (left/right arrows): Switch between buttons
- `Enter`: Confirm focused button
- `Y`: Quick confirm (if destructive action)
- `N` or `Esc`: Quick cancel

**Template**:
```go
func (a *App) renderConfirmationDialog() string {
    // Dialog dimensions
    dialogWidth := 50

    var content strings.Builder

    // ── Title (warning/important) ──────────────────────────────────
    titleStyle := lipgloss.NewStyle().
        Foreground(lipgloss.Color(a.theme.Colors.Red)).  // Red for destructive, Yellow for caution
        Bold(true).
        Align(lipgloss.Center).
        Width(dialogWidth - 4)

    content.WriteString(titleStyle.Render("▲  Confirm Action"))
    content.WriteString("\n\n")

    // ── Message ────────────────────────────────────────────────────
    msgStyle := lipgloss.NewStyle().
        Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
        Width(dialogWidth - 4).
        Align(lipgloss.Center)

    content.WriteString(msgStyle.Render("Are you sure?\n\nThis action cannot be undone."))
    content.WriteString("\n\n")

    // ── Buttons ────────────────────────────────────────────────────
    // Button 0: Primary action (destructive)
    confirmBtn := a.renderDialogButton(
        "Yes, Delete",
        state.focusedButton == 0,
        lipgloss.Color(a.theme.Colors.Red),
        true, // destructive
    )

    // Button 1: Cancel
    cancelBtn := a.renderDialogButton(
        "No, Cancel",
        state.focusedButton == 1,
        lipgloss.Color(a.theme.Colors.Comment),
        false, // not destructive
    )

    buttonRow := lipgloss.JoinHorizontal(lipgloss.Center, confirmBtn, "  ", cancelBtn)
    buttonRowStyle := lipgloss.NewStyle().
        Width(dialogWidth - 4).
        Align(lipgloss.Center)
    content.WriteString(buttonRowStyle.Render(buttonRow))
    content.WriteString("\n\n")

    // ── Help Text ──────────────────────────────────────────────────
    helpStyle := lipgloss.NewStyle().
        Foreground(lipgloss.Color(a.theme.Colors.Comment)).
        Italic(true).
        Width(dialogWidth - 4).
        Align(lipgloss.Center)
    content.WriteString(helpStyle.Render("[Tab/←→] Switch  [Enter] Confirm  [Y] Yes  [N/Esc] Cancel"))

    // ── Wrap in dialog box ─────────────────────────────────────────
    dialogStyle := lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color(a.theme.Colors.Red)). // Match title color
        Padding(1, 2).
        Width(dialogWidth)

    dialog := dialogStyle.Render(content.String())

    // Center on screen
    return lipgloss.NewStyle().
        Width(a.width).
        Height(a.height).
        Align(lipgloss.Center, lipgloss.Center).
        Render(dialog)
}

// renderDialogButton renders a button with consistent styling
func (a *App) renderDialogButton(label string, focused bool, color lipgloss.Color, destructive bool) string {
    if focused {
        // Focused button: filled background
        return lipgloss.NewStyle().
            Foreground(lipgloss.Color(a.theme.Colors.Background)).
            Background(color).
            Bold(true).
            Padding(0, 2).
            Render(label)
    } else if destructive {
        // Unfocused destructive button: colored text, no border
        return lipgloss.NewStyle().
            Foreground(color).
            Padding(0, 2).
            Render(label)
    } else {
        // Unfocused normal button: normal text, no border
        return lipgloss.NewStyle().
            Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
            Padding(0, 2).
            Render(label)
    }
}
```

**Key Handler**:
```go
func (a *App) handleConfirmDialogKey(msg tea.KeyMsg) tea.Cmd {
    switch msg.String() {
    case "tab", "right":
        // Next button
        state.focusedButton = (state.focusedButton + 1) % 2
        return nil

    case "shift+tab", "left":
        // Previous button
        state.focusedButton--
        if state.focusedButton < 0 {
            state.focusedButton = 1
        }
        return nil

    case "enter":
        // Confirm focused button
        if state.focusedButton == 0 {
            return a.handleConfirmed()
        } else {
            return a.handleCanceled()
        }

    case "y", "Y":
        // Quick confirm (destructive action)
        return a.handleConfirmed()

    case "n", "N", "esc":
        // Quick cancel
        return a.handleCanceled()
    }
    return nil
}
```

---

### 2. **Information Modal** (Help, About, etc.)
**Use for**: Read-only information, help text, documentation
**Examples**: /help command, keyboard shortcuts reference

**Pattern**:
- Overlay on top of base view (dimmed background optional)
- Scrollable content area
- Header at top
- Footer with close instruction

**Navigation**:
- `↑↓` or `PgUp/PgDn`: Scroll content
- `Esc`: Close

**Template**:
```go
func (a *App) renderInfoModalOverlay(baseView string) string {
    overlayWidth := 80
    if overlayWidth > a.width-4 {
        overlayWidth = a.width - 4
    }

    // Header
    headerStyle := lipgloss.NewStyle().
        Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
        Background(lipgloss.Color(a.theme.Colors.Background)).
        Bold(true).
        Align(lipgloss.Center).
        Width(overlayWidth - 2)
    header := headerStyle.Render("Information Title")

    // Content (scrollable viewport recommended)
    contentStyle := lipgloss.NewStyle().
        Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
        Background(lipgloss.Color(a.theme.Colors.Background)).
        Width(overlayWidth - 4)
    content := contentStyle.Render(state.Content)

    // Footer
    hintStyle := lipgloss.NewStyle().
        Foreground(lipgloss.Color(a.theme.Colors.Comment)).
        Background(lipgloss.Color(a.theme.Colors.Background)).
        Italic(true).
        Align(lipgloss.Center).
        Width(overlayWidth - 2)
    hints := hintStyle.Render("Esc: Close")

    // Assemble
    var modalContent strings.Builder
    modalContent.WriteString(header + "\n\n")
    modalContent.WriteString(content)
    modalContent.WriteString("\n\n" + hints)

    // Wrap in border
    modalStyle := lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color(a.theme.Colors.Cyan)).
        Padding(1, 2).
        Width(overlayWidth).
        Background(lipgloss.Color(a.theme.Colors.Background))

    modal := modalStyle.Render(modalContent.String())

    // Overlay on base view (centered)
    return lipgloss.PlaceHorizontal(
        a.width,
        lipgloss.Center,
        lipgloss.PlaceVertical(a.height, lipgloss.Center, modal),
    )
}
```

---

### 3. **Selection List Modal** (Links, Files, etc.)
**Use for**: Choose from list of items
**Examples**: Link browser, file picker

**Pattern**:
- List of items with selection cursor
- Numbered or bulleted
- Selected item highlighted
- Footer with navigation hints

**Navigation**:
- `↑↓` or `j/k`: Navigate list
- `Enter` or number key: Select item
- `Esc`: Cancel/close

**Template**:
```go
func (a *App) renderSelectionListOverlay(baseView string) string {
    overlayWidth := 80

    // Header
    headerStyle := lipgloss.NewStyle().
        Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
        Background(lipgloss.Color(a.theme.Semantic.InputBg)).
        Bold(true).
        Align(lipgloss.Center).
        Width(overlayWidth - 2)
    header := headerStyle.Render("Select Item")

    // List items
    var items []string
    for i, item := range state.Items {
        numberStyle := lipgloss.NewStyle().
            Foreground(lipgloss.Color(a.theme.Colors.Comment)).
            Background(lipgloss.Color(a.theme.Semantic.InputBg)).
            Bold(true)
        itemStyle := lipgloss.NewStyle().
            Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
            Background(lipgloss.Color(a.theme.Semantic.InputBg))

        // Highlight selected
        if i == state.SelectedIndex {
            numberStyle = numberStyle.Background(lipgloss.Color(a.theme.Colors.Selection))
            itemStyle = itemStyle.Background(lipgloss.Color(a.theme.Colors.Selection))
        }

        line := fmt.Sprintf("%s %s",
            numberStyle.Render(fmt.Sprintf("[%d]", i+1)),
            itemStyle.Render(item))

        lineStyle := lipgloss.NewStyle().
            Background(lipgloss.Color(a.theme.Semantic.InputBg)).
            Width(overlayWidth - 2)
        items = append(items, lineStyle.Render(line))
    }

    // Footer
    hintStyle := lipgloss.NewStyle().
        Foreground(lipgloss.Color(a.theme.Colors.Comment)).
        Background(lipgloss.Color(a.theme.Semantic.InputBg)).
        Italic(true).
        Align(lipgloss.Center).
        Width(overlayWidth - 2)
    hints := hintStyle.Render("↑↓: Navigate · Enter: Select · Esc: Cancel")

    // Assemble
    var content strings.Builder
    content.WriteString(header + "\n\n")
    content.WriteString(strings.Join(items, "\n"))
    content.WriteString("\n\n" + hints)

    // Wrap
    modalStyle := lipgloss.NewStyle().
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color(a.theme.Colors.Cyan)).
        Padding(1, 2).
        Width(overlayWidth).
        Background(lipgloss.Color(a.theme.Semantic.InputBg))

    return lipgloss.PlaceHorizontal(a.width, lipgloss.Center,
        lipgloss.PlaceVertical(a.height, lipgloss.Center,
            modalStyle.Render(content.String())))
}
```

---

### 4. **Multi-Button Dialog** (3+ options)
**Use for**: Multiple choice actions
**Examples**: Terms of Service (Skip to End, Accept, Decline)

**Pattern**:
- Horizontal row of buttons
- Tab or arrow keys to navigate
- Enter to confirm
- Some buttons may be disabled until conditions met

**Navigation**:
- `Tab` or `→`: Next button
- `Shift+Tab` or `←`: Previous button
- `Enter`: Confirm focused button
- Letter shortcuts: Optional quick actions (A, D, S)

**Template**:
```go
func (a *App) renderMultiButtonDialog() string {
    buttons := []string{"Option A", "Option B", "Option C"}

    var renderedButtons []string
    buttonTextWidth := 10 // Max button label length

    for i, label := range buttons {
        var style lipgloss.Style

        if state.focusedButton == i {
            // Focused
            style = lipgloss.NewStyle().
                Foreground(lipgloss.Color(a.theme.Colors.Background)).
                Background(lipgloss.Color(a.theme.Colors.Purple)).
                Bold(true).
                Padding(0, 2).
                MarginLeft(2).
                MarginRight(2)
        } else if state.buttonEnabled[i] {
            // Enabled but not focused
            style = lipgloss.NewStyle().
                Foreground(lipgloss.Color(a.theme.Colors.Foreground)).
                Padding(0, 2).
                MarginLeft(2).
                MarginRight(2)
        } else {
            // Disabled
            style = lipgloss.NewStyle().
                Foreground(lipgloss.Color(a.theme.Colors.Comment)).
                Padding(0, 2).
                MarginLeft(2).
                MarginRight(2)
        }

        paddedLabel := lipgloss.PlaceHorizontal(buttonTextWidth, lipgloss.Center, label)
        renderedButtons = append(renderedButtons, style.Render(paddedLabel))
    }

    return lipgloss.JoinHorizontal(lipgloss.Top, renderedButtons...)
}
```

---

## Common Patterns & Best Practices

### Dimensions
- **Small dialog** (confirmation): 50 chars wide
- **Medium dialog** (forms): 60-70 chars wide
- **Large modal** (help, info): 80-100 chars wide
- **Height**: Auto-sized to content, cap at `a.height - 4` to prevent overflow

### Colors
- **Destructive actions**: Red border, red button background
- **Caution**: Yellow/Orange border
- **Info/Neutral**: Cyan or Purple border
- **Success**: Green border

### Padding
- **Inside border**: `Padding(1, 2)` (1 vertical, 2 horizontal)
- **Between buttons**: 2 spaces (`"  "`)
- **Margins**: Use sparingly; prefer padding

### Help Text
- Always include help text at bottom showing available keys
- Format: `[Key] Action · [Key] Action`
- Style: Comment color, italic, centered
- Example: `[Tab/←→] Switch  [Enter] Confirm  [Esc] Cancel`

### Centering
```go
lipgloss.NewStyle().
    Width(a.width).
    Height(a.height).
    Align(lipgloss.Center, lipgloss.Center).
    Render(dialog)
```

### Overlay Pattern
Use when dialog appears on top of another view:
```go
func (a *App) renderDialogOverlay(baseView string) string {
    // Render dialog
    dialog := a.renderDialog()

    // Option 1: Simple overlay (dialog appears on top, base view still visible)
    return lipgloss.PlaceHorizontal(a.width, lipgloss.Center,
        lipgloss.PlaceVertical(a.height, lipgloss.Center, dialog))

    // Option 2: Full replace (dialog takes over entire screen)
    return dialog
}
```

---

## State Management

All dialogs should have state tracking:
```go
type ConfirmDialogState struct {
    Open          bool
    Title         string
    Message       string
    FocusedButton int  // 0 = first button, 1 = second button
    OnConfirm     func() tea.Cmd
    OnCancel      func() tea.Cmd
}
```

---

## Keyboard Navigation Standards

| Key | Action | Notes |
|-----|--------|-------|
| `Tab` | Next button/field | Standard web pattern |
| `Shift+Tab` | Previous button/field | Reverse direction |
| `←` | Previous button | Intuitive for horizontal layout |
| `→` | Next button | Intuitive for horizontal layout |
| `↑↓` | Navigate list items | For selection lists only |
| `Enter` | Confirm focused action | Primary action |
| `Esc` | Cancel/close | Universal escape |
| `Y` | Quick yes/confirm | Destructive actions only |
| `N` | Quick no/cancel | Destructive actions only |
| `PgUp/PgDn` | Scroll content | For scrollable modals |

---

## Examples in Codebase

✅ **Terms of Service Dialog** - [tos_view.go:242-330](tos_view.go#L242-L330)
- 3-button layout with Tab navigation
- Buttons disabled until scroll condition met
- Example of multi-button pattern

✅ **Help Modal** - [views.go:1704-1780](views.go#L1704-L1780)
- Overlay pattern
- Read-only information display
- Esc to close

✅ **Link Browser** - [views.go:1598-1700](views.go#L1598-L1700)
- Selection list pattern
- Arrow key navigation
- Enter to confirm

✅ **Delete Confirmation Dialog** - [server_management_view.go:3066-3162](server_management_view.go#L3066-L3162)
- 2-button layout with Tab/arrow navigation
- Background fill for focused button, plain text for unfocused
- Example of confirmation dialog pattern

---

## Migration Checklist

When updating an existing dialog to match this template:
- [ ] Add `focusedButton` field to state struct
- [ ] Implement Tab/left-right navigation in key handler
- [ ] Update button rendering to show focus state
- [ ] Update help text to show all navigation options
- [ ] Test all keyboard shortcuts (Tab, arrows, Enter, Esc, letter shortcuts)
- [ ] Ensure button focus wraps around (0 → 1 → 0)
- [ ] Verify consistent styling (colors, padding, borders)

---

Last Updated: 2026-03-11
