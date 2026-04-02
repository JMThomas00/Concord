# Settings Page Template Guide

This guide explains how to create consistent settings pages for both Client Settings (Ctrl+S) and Server Settings (Ctrl+B) that maintain perfect border alignment.

---

## Quick Reference: The +2 Padding Rule

**Critical**: All settings pages MUST allocate **+2 extra lines** in the top section beyond what they write. This padding is required for proper border alignment.

### Formula

```go
pageTopExtra = (lines you actually write) - (base 4 lines) + 2
```

**Examples**:
- Write 4 lines (header+subtitle+blank+sep) → `pageTopExtra = 2`
- Write 5 lines (header+subtitle+stats+blank+sep) → `pageTopExtra = 3`
- Write 6 lines (header+subtitle+stats+filter+blank+sep) → `pageTopExtra = 4`

**Bottom section**: Count your help lines
- 1 help line → `pageBottomExtra = 0` (separator + help = 2 lines)
- 2 help lines → `pageBottomExtra = 1` (separator + blank + 2 help = 4 lines)

---

## Template Function

```go
// renderExampleSettingsPage demonstrates the standard pattern for settings pages.
// Copy this function and customize the content for your new page.
func (a *App) renderExampleSettingsPage(width, height int) string {
    // ═══════════════════════════════════════════════════════════════
    // STEP 1: Calculate Layout
    // ═══════════════════════════════════════════════════════════════

    // Count your top section lines (see below), then add +2 for padding
    // Example: 4 lines written + 2 padding = pageTopExtra: 2
    const pageTopExtra = 2

    // Count your bottom help lines: 0 for 1 help line, 1 for 2 help lines
    const pageBottomExtra = 0

    layout := calculateSettingsLayout(width, height, pageTopExtra, pageBottomExtra)

    // ═══════════════════════════════════════════════════════════════
    // STEP 2: Build TOP Section
    // ═══════════════════════════════════════════════════════════════

    top := newSectionBuilder(layout.topLines, layout.interiorWidth)

    // Header (line 1)
    headerStyle := lipgloss.NewStyle().
        Foreground(lipgloss.Color(a.theme.Colors.Cyan)).
        Bold(true)
    top.writeLine(headerStyle.Render("Page Title"))

    // Subtitle (line 2)
    subtitleStyle := lipgloss.NewStyle().
        Foreground(lipgloss.Color(a.theme.Colors.Comment))
    top.writeLine(subtitleStyle.Render("Brief description of this page"))

    // Optional: Stats/Status line (line 3)
    // statsStyle := lipgloss.NewStyle().
    //     Foreground(lipgloss.Color(a.theme.Colors.Comment))
    // top.writeLine(statsStyle.Render("Status or count information"))

    // Blank line (line 3 or 4, depending on stats)
    top.writeBlank()

    // Separator (line 4 or 5)
    top.writeLine(a.renderSeparator(layout.interiorWidth))

    // NOTE: Don't call top.pad() - padding is automatic with separator as last line

    // ═══════════════════════════════════════════════════════════════
    // STEP 3: Build MIDDLE Section (Scrollable Content)
    // ═══════════════════════════════════════════════════════════════

    middle := newSectionBuilder(layout.middleLines, layout.interiorWidth)

    // Add your main content here
    contentStyle := lipgloss.NewStyle().
        Foreground(lipgloss.Color(a.theme.Colors.Foreground))

    middle.writeLine(contentStyle.Render("Your content here"))
    middle.writeLine(contentStyle.Render("This area scrolls if needed"))
    middle.writeBlank()
    middle.writeLine(contentStyle.Render("Add forms, lists, or other UI elements"))

    // CRITICAL: Always pad the middle section to fill remaining space
    middle.pad()

    // ═══════════════════════════════════════════════════════════════
    // STEP 4: Build BOTTOM Section (Help Text)
    // ═══════════════════════════════════════════════════════════════

    bottom := newSectionBuilder(layout.bottomLines, layout.interiorWidth)

    // Separator
    bottom.writeLine(a.renderSeparator(layout.interiorWidth))

    // Help text (1 or 2 lines based on pageBottomExtra)
    helpStyle := lipgloss.NewStyle().
        Foreground(lipgloss.Color(a.theme.Colors.Comment))

    // If pageBottomExtra = 0: Use 1 help line
    bottom.writeLine(helpStyle.Render("Navigation: ↑↓ select · Enter confirm · Esc back"))

    // If pageBottomExtra = 1: Add blank + 2 help lines
    // bottom.writeBlank()
    // bottom.writeLine(helpStyle.Render("Line 1: First set of shortcuts"))
    // bottom.writeLine(helpStyle.Render("Line 2: Second set of shortcuts"))

    // CRITICAL: Always pad the bottom section
    bottom.pad()

    // ═══════════════════════════════════════════════════════════════
    // STEP 5: Assemble All Sections
    // ═══════════════════════════════════════════════════════════════

    content := lipgloss.JoinVertical(lipgloss.Left,
        top.String(),
        middle.String(),
        bottom.String(),
    )

    // ═══════════════════════════════════════════════════════════════
    // STEP 6: Render with Border and Padding
    // ═══════════════════════════════════════════════════════════════

    // CRITICAL: ALWAYS use .Padding(0, 1) for settings content pages
    return lipgloss.NewStyle().
        Width(width).
        Height(height).
        Border(lipgloss.RoundedBorder()).
        BorderForeground(lipgloss.Color(a.theme.Colors.Selection)).  // or .Purple
        Padding(0, 1).  // REQUIRED for proper alignment
        Render(content)
}
```

---

## Line Counting Reference

### Top Section (Base = 4 lines)

**Standard Pattern** (4 lines):
```go
top.writeLine(header)      // 1
top.writeLine(subtitle)    // 2
top.writeBlank()           // 3
top.writeLine(separator)   // 4
```
→ `pageTopExtra = 2` (4 written + 2 padding)

**With Stats** (5 lines):
```go
top.writeLine(header)      // 1
top.writeLine(subtitle)    // 2
top.writeLine(stats)       // 3 (EXTRA)
top.writeBlank()           // 4
top.writeLine(separator)   // 5
```
→ `pageTopExtra = 3` (5 written + 2 padding)

**With Stats + Filter** (6 lines):
```go
top.writeLine(header)      // 1
top.writeLine(subtitle)    // 2
top.writeLine(stats)       // 3 (EXTRA)
top.writeLine(filter)      // 4 (EXTRA)
top.writeBlank()           // 5
top.writeLine(separator)   // 6
```
→ `pageTopExtra = 4` (6 written + 2 padding)

### Bottom Section (Base = 3 lines)

**Single Help Line** (2 lines):
```go
bottom.writeLine(separator)  // 1
bottom.writeLine(help)       // 2
```
→ `pageBottomExtra = 0` (allocates 3, writes 2, 1 line auto-padding)

**Double Help Line** (4 lines):
```go
bottom.writeLine(separator)  // 1
bottom.writeBlank()          // 2
bottom.writeLine(help1)      // 3
bottom.writeLine(help2)      // 4
```
→ `pageBottomExtra = 1` (allocates 4, writes 4, 0 line auto-padding)

---

## Common Patterns

### Category List Page (e.g., Channels, Roles, Members)

```go
layout := calculateSettingsLayout(width, height, 3, 1)  // 5 top lines + 2 padding, 4 bottom lines

// Top: header + subtitle + stats + blank + separator = 5 lines
// Bottom: separator + blank + 2 help lines = 4 lines
```

### Form Page (e.g., Create Channel, Edit Channel)

```go
layout := calculateSettingsLayout(width, height, 2, 0)  // 4 top lines + 2 padding, 2 bottom lines

// Top: header + subtitle + blank + separator = 4 lines
// Bottom: separator + 1 help line = 2 lines
```

### Simple Info Page (e.g., Messages/Retention)

```go
layout := calculateSettingsLayout(width, height, 2, 1)  // 4 top lines + 2 padding, 4 bottom lines

// Top: header + subtitle + blank + separator = 4 lines (no stats)
// Bottom: separator + blank + 2 help lines = 4 lines
```

---

## Sidebar Notes

**DO NOT** apply this template to sidebars! Sidebars use a different pattern:

```go
// Sidebar (renderCategorySidebar pattern)
return lipgloss.NewStyle().
    Width(width).
    Height(height).
    Border(lipgloss.RoundedBorder()).
    BorderForeground(lipgloss.Color(a.theme.Colors.Purple)).
    Render(buf.String())  // NO .Padding() on sidebar!
```

Sidebars:
- ✅ Use `.Border()`
- ❌ Do NOT use `.Padding()`
- ✅ Use `width - 2` for interior content (border only)

---

## Checklist for New Settings Page

- [ ] Count your top section lines (header, subtitle, stats/status, blank, separator)
- [ ] Add +2 to get `pageTopExtra` value
- [ ] Count your bottom help lines (1 or 2)
- [ ] Set `pageBottomExtra` (0 for 1 line, 1 for 2 lines)
- [ ] Use `newSectionBuilder()` for top, middle, bottom sections
- [ ] Call `.pad()` on middle and bottom sections
- [ ] Use `lipgloss.JoinVertical()` to assemble sections
- [ ] Apply `.Border().Padding(0, 1)` to final render
- [ ] Test border alignment by switching between pages
- [ ] Verify top border is fully visible (not cut off)
- [ ] Verify bottom borders align with sidebar

---

## Why This Works

The **+2 padding pattern** ensures:
1. Consistent top section heights across all pages
2. Proper vertical alignment when sections are joined
3. No border cutoff (total height fits exactly in allocated space)
4. Smooth transitions when navigating between pages

Without the +2 padding, content would overflow allocated space, causing lipgloss to truncate and cut off top borders.

---

## Examples from Codebase

### Working Examples

✅ `renderChannelsCategory` - [server_management_view.go:1066](server_management_view.go#L1066)
```go
layout := calculateSettingsLayout(width, height, 3, 1)
// 5 lines top (header+subtitle+stats+blank+sep) + 2 padding = pageTopExtra: 3
// 4 lines bottom (sep+blank+help+help) = pageBottomExtra: 1
```

✅ `renderRolesCategory` - [server_management_view.go:1288](server_management_view.go#L1288)
```go
layout := calculateSettingsLayout(width, height, 3, 1)
// Same as Channels
```

✅ `renderChannelFormPage` - [server_management_view.go:1725](server_management_view.go#L1725)
```go
layout := calculateSettingsLayout(width, height, 2, 0)
// 4 lines top (header+subtitle+blank+sep) + 2 padding = pageTopExtra: 2
// 2 lines bottom (sep+help) = pageBottomExtra: 0
```

### Reference

For Client Settings pages, see:
- `renderThemeContent` - [settings_view.go:640](settings_view.go#L640)
- `renderNotificationsContent` - Similar pattern
- `renderDisplayContent` - Similar pattern

All follow the same +2 padding rule for perfect border alignment.
