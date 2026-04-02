package server

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

const banner = `

                                                                        
   ██████╗ ██████╗ ███╗   ██╗ ██████╗ ██████╗ ██████╗ ██████╗           
  ██╔════╝██╔═══██╗████╗  ██║██╔════╝██╔═══██╗██╔══██╗██╔══██╗          
  ██║     ██║   ██║██╔██╗ ██║██║     ██║   ██║██████╔╝██║  ██║          
  ██║     ██║   ██║██║╚██╗██║██║     ██║   ██║██╔══██╗██║  ██║          
  ╚██████╗╚██████╔╝██║ ╚████║╚██████╗╚██████╔╝██║  ██║██████╔╝          
   ╚═════╝ ╚═════╝ ╚═╝  ╚═══╝ ╚═════╝ ╚═════╝ ╚═╝  ╚═╝╚═════╝           
   🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇🍇         
   
   Concord Server v0.1.0
   Terminal Chat Reimagined

`

// PrintBanner displays the beautiful ASCII art banner
func PrintBanner() {
	style := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#BD93F9")). // Purple
		Bold(true)

	fmt.Println(style.Render(banner))
}

// PrintStartupInfo displays colorful startup information
func PrintStartupInfo(addr string, dbPath string) {
	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")). // Cyan
		Bold(true)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("213")) // Pink

	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("255")). // White
		Bold(true)

	checkStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")). // Green checkmark
		Bold(true)

	fmt.Println(headerStyle.Render("📦 Initializing components..."))
	fmt.Printf("  %s %-15s %s\n",
		labelStyle.Render("💾"),
		"Database",
		checkStyle.Render("✅ Connected ("+dbPath+")"))
	fmt.Printf("  %s %-15s %s\n",
		labelStyle.Render("🔐"),
		"Authentication",
		checkStyle.Render("✅ JWT tokens enabled"))
	fmt.Printf("  %s %-15s %s\n",
		labelStyle.Render("📡"),
		"WebSocket",
		checkStyle.Render("✅ Ready on "+addr))
	fmt.Printf("  %s %-15s %s\n",
		labelStyle.Render("🔄"),
		"Message Hub",
		checkStyle.Render("✅ Started"))
	fmt.Printf("  %s %-15s %s\n",
		labelStyle.Render("⏰"),
		"Pruning Service",
		checkStyle.Render("✅ Scheduled (daily)"))
	fmt.Println()

	fmt.Println(headerStyle.Render("🌐 Endpoints:"))
	fmt.Printf("  • HTTP:      %s\n", valueStyle.Render("http://"+addr))
	fmt.Printf("  • WebSocket: %s\n", valueStyle.Render("ws://"+addr+"/ws"))
	fmt.Println()

	successStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("86")). // Cyan
		Bold(true)
	fmt.Println(successStyle.Render("✨ Server ready to accept connections!"))
	fmt.Println()
}
