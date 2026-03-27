# Concord UI Symbols Reference

This document defines all Unicode symbols and glyphs used in Concord's terminal UI to ensure consistency and cross-terminal compatibility.

---

## Design Philosophy

Concord uses **terminal-friendly Unicode symbols** instead of full-color emoji for:
- **Consistent rendering** across all terminals (Windows Terminal, iTerm2, Kitty, etc.)
- **Monospace alignment** - symbols don't break layout
- **Performance** - faster rendering than emoji
- **Aesthetic** - fits terminal app style
- **Accessibility** - works in text-only terminals

---

## Symbol Reference

### Channel Types

| Symbol | Unicode | Name | Usage |
|--------|---------|------|-------|
| `#` | U+0023 | Hash | Text channels |
| `♪` | U+266A | Music note | Voice channels |

**Examples:**
```
# general
# random
♪ General Voice
♪ Gaming
```

### Channel States

| Symbol | Unicode | Name | Usage |
|--------|---------|------|-------|
| `⊗` | U+2297 | Circled times | Locked channel (read-only) |
| `●` | U+25CF | Black circle | Unread messages indicator |
| `@N` | - | At sign + number | Unread @mentions count |

**Examples:**
```
# ⊗ announcements
# general ●
# off-topic @3
```

### Navigation & Hierarchy

| Symbol | Unicode | Name | Usage |
|--------|---------|------|-------|
| `▶` | U+25B6 | Right triangle | Collapsed category / Selected item |
| `▼` | U+25BC | Down triangle | Expanded category |
| `─` | U+2500 | Box drawing light horizontal | Category separator |

**Examples:**
```
▼ TEXT CHANNELS
  # general
  # random
▶ VOICE CHANNELS
```

### Server & Member Status

| Symbol | Unicode | Name | Usage |
|--------|---------|------|-------|
| `●` | U+25CF | Black circle | Online status / Connected server |
| `○` | U+25CB | White circle | Offline status / Disconnected server |

**Examples:**
```
(A) alice    ●
(B) bob      ○
```

### Actions & Indicators

| Symbol | Unicode | Name | Usage |
|--------|---------|------|-------|
| `★` | U+2606 | White star | Pinned messages header |
| `▲` | U+25B2 | Black triangle | Delete confirmations, warnings |
| `☐` | U+2610 | Ballot box | Unchecked checkbox |
| `☑` | U+2611 | Ballot box with check | Checked checkbox |
| `✓` | U+2713 | Check mark | Enabled permission / Success |

**Examples:**
```
★ 3 pinned message(s)  (/unpin N to remove)
▲  Confirm Deletion
☑ Show separately in members list
☐ Allow anyone to @mention this role
```

### Form & Dialog Elements

| Symbol | Unicode | Name | Usage |
|--------|---------|------|-------|
| `[ ]` | - | Brackets | Unchecked (alternative) |
| `[✓]` | - | Brackets + check | Checked permission |
| `(●)` | - | Parentheses + circle | Selected radio button |
| `( )` | - | Parentheses | Unselected radio button |

**Examples:**
```
Permission Preset:
  (●) Members - Basic chat permissions
  ( ) Moderator - Moderation + chat

Permissions:
  [✓] Send Messages
  [ ] Manage Channels
```

---

## Avoided Emoji

These full-color emoji were **replaced** with terminal-friendly symbols:

| ❌ Removed | ✅ Replaced With | Reason |
|-----------|-----------------|--------|
| 📌 (pushpin) | `★` (star) | Universal, renders consistently |
| 🔒 (locked padlock) | `⊗` (circled X) | Cleaner, monospace-friendly |
| 🔊 (speaker) | `♪` (music note) | Symbolic, terminal-safe |

---

## Color Usage

Symbols are **never hard-coded with color**. Color is applied via lipgloss styles based on theme:

```go
// Good: Theme-aware coloring
lockIcon := "⊗"
lockStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(a.theme.Colors.Red))
styledIcon := lockStyle.Render(lockIcon)

// Bad: Hard-coded colors or emoji
lockIcon := "🔒" // Emoji color can't be controlled
```

---

## Testing Checklist

When adding new symbols:
- [ ] Test in Windows Terminal (Windows)
- [ ] Test in iTerm2/Terminal.app (macOS)
- [ ] Test in Kitty/Alacritty (Linux)
- [ ] Verify monospace alignment (no width overflow)
- [ ] Check contrast in both light/dark themes
- [ ] Confirm Unicode codepoint is widely supported (< U+2700 preferred)

---

## Unicode Ranges Used

| Range | Name | Example Symbols |
|-------|------|----------------|
| U+0020-007E | Basic Latin | `#`, `@`, `*` |
| U+2190-21FF | Arrows | `→`, `←`, `↑`, `↓` |
| U+2200-22FF | Mathematical Operators | `⊗` |
| U+2500-257F | Box Drawing | `─`, `│`, `┌`, `└` |
| U+25A0-25FF | Geometric Shapes | `●`, `○`, `▶`, `▼` |
| U+2600-26FF | Miscellaneous Symbols | `⚠`, `★`, `☐`, `☑` |
| U+2660-26FF | Music Symbols | `♪`, `♫` |

**Avoid:**
- U+1F300-1F9FF (Emoji block) - unreliable rendering
- U+E000-F8FF (Private Use Area) - not standardized

---

## Migration Guide

When replacing emoji with symbols:

1. **Find all instances**:
   ```bash
   grep -r "🔒\|🔊\|📌" internal/client
   ```

2. **Replace with terminal-friendly symbol**:
   ```diff
   - prefix = "🔊 "
   + prefix = "♪ "
   ```

3. **Test rendering** in multiple terminals

4. **Update this document** with the new symbol

---

## Related Documents

- [DIALOG_TEMPLATE.md](DIALOG_TEMPLATE.md) - Dialog/modal patterns
- [SETTINGS_PAGE_TEMPLATE.md](SETTINGS_PAGE_TEMPLATE.md) - Settings page layouts

---

Last Updated: 2026-03-11
