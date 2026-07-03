package hub

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"
)

const banner = `

    ██████╗ ██████╗  █████╗ ██████╗ ███████╗██╗   ██╗██╗███╗   ██╗███████╗
   ██╔════╝ ██╔══██╗██╔══██╗██╔══██╗██╔════╝██║   ██║██║████╗  ██║██╔════╝
   ██║  ███╗██████╔╝███████║██████╔╝█████╗  ██║   ██║██║██╔██╗ ██║█████╗
   ██║   ██║██╔══██╗██╔══██║██╔═══╝ ██╔══╝  ╚██╗ ██╔╝██║██║╚██╗██║██╔══╝
   ╚██████╔╝██║  ██║██║  ██║██║     ███████╗ ╚████╔╝ ██║██║ ╚████║███████╗
    ╚═════╝ ╚═╝  ╚═╝╚═╝  ╚═╝╚═╝     ╚══════╝  ╚═══╝  ╚═╝╚═╝  ╚═══╝╚══════╝
   🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇

   Grapevine Hub v0.1.0
   Concord Server Discovery

`

// PrintBanner displays the ASCII art banner (plain, non-dashboard mode).
func PrintBanner() {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#BD93F9")). // Purple
		Bold(true)

	fmt.Println(style.Render(banner))
}

// PrintStartupInfo displays colorful startup information for plain
// (non-dashboard) mode, mirroring the Concord server's startup screen.
func (h *Hub) PrintStartupInfo() {
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")). // Cyan
		Bold(true)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("213")) // Pink

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("255")). // White
		Bold(true)

	checkStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")).
		Bold(true)

	warnStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("221")). // Yellow
		Bold(true)

	item := func(emoji, label, status string) {
		fmt.Printf("  %s %-15s %s\n", labelStyle.Render(emoji), label, status)
	}

	listed, online := 0, 0
	if servers, err := h.db.ListServers("", "", true); err == nil {
		listed = len(servers)
		for _, sv := range servers {
			if sv.IsOnline {
				online++
			}
		}
	}

	activePeers := 0
	if peers, err := h.db.ListPeerHubs(); err == nil {
		for _, p := range peers {
			if p.IsActive {
				activePeers++
			}
		}
	}

	fmt.Println(headerStyle.Render("📦 Initializing components..."))
	item("💾", "Database", checkStyle.Render("✅ Connected ("+h.config.DatabasePath+")"))
	item("📖", "Registry", checkStyle.Render(fmt.Sprintf("✅ %d server(s) listed, %d online", listed, online)))
	item("💓", "Heartbeats", checkStyle.Render(fmt.Sprintf("✅ Offline after %ds of silence", h.config.HeartbeatTimeout)))
	item("🧹", "Cleanup", checkStyle.Render(fmt.Sprintf("✅ Purging listings offline > %d days", int(stalePurgeAge/(24*time.Hour)))))
	if activePeers > 0 {
		item("🕸️", "Federation", checkStyle.Render(fmt.Sprintf("✅ %d peer hub(s), syncing every %dm", activePeers, h.config.FederationSync)))
	} else {
		item("🕸️", "Federation", checkStyle.Render("✅ Ready (no peer hubs yet)"))
	}
	if h.config.AdminToken != "" {
		item("🔑", "Admin API", checkStyle.Render("✅ Token configured"))
	} else {
		item("🔑", "Admin API", warnStyle.Render("⚠️ Disabled (no admin_token — re-run --setup to add one)"))
	}
	fmt.Println()

	base := "http://" + h.srv.Addr
	fmt.Println(headerStyle.Render("🌐 Endpoints:"))
	fmt.Printf("  • Health:   %s\n", valueStyle.Render(base+"/v1/health"))
	fmt.Printf("  • Listing:  %s\n", valueStyle.Render(base+"/v1/servers"))
	fmt.Printf("  • Register: %s\n", valueStyle.Render(base+"/v1/servers (POST)"))
	fmt.Println()

	successStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")).
		Bold(true)
	fmt.Println(successStyle.Render("✨ " + h.config.HubName + " is ready — servers can join the vine!"))
	fmt.Println()
}
