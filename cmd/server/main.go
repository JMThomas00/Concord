package main

import (
	"flag"
	"fmt"
	"golang.org/x/term"
	"os"
	"strings"

	charmlog "github.com/charmbracelet/log"
	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/server"
	"github.com/pelletier/go-toml/v2"
)

// Version/GitCommit/BuildTime are populated at build time via the
// Makefile's shared LDFLAGS ("-X main.Version=..." etc.) -- reported to
// connecting clients via ReadyPayload, see Server Settings > About.
// Defaults here keep a plain `go build`/`go run` (no ldflags) sensible.
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildTime = "unknown"
)

func main() {
	// Parse command line flags
	configPath := flag.String("config", "", "Path to configuration file")
	host := flag.String("host", "", "Host to bind to (overrides config)")
	port := flag.Int("port", 0, "Port to bind to (overrides config)")
	dbPath := flag.String("db", "", "Path to database file (overrides config)")
	adminEmail := flag.String("admin-email", "", "Grant admin role to this email on startup")
	fixAdmin := flag.Bool("fix-admin", false, "Repair broken Admin role (permissions, is_hoisted)")
	cleanupDuplicates := flag.Bool("cleanup-duplicates", false, "Merge and remove duplicate roles")
	logLevel := flag.String("log-level", "info", "Log level (debug, info, warn, error)")
	debugMode := flag.Bool("debug", false, "Enable debug logging (shorthand for --log-level debug)")
	hybridMode := flag.Bool("hybrid", false, "Enable hybrid dashboard with live logs")
	dashboardOnlyMode := flag.Bool("dashboard", false, "Enable full-screen dashboard mode (no live logs)")
	reconfigure := flag.Bool("reconfigure", false, "Re-run setup wizard to reconfigure server")
	testMail := flag.String("test-mail", "", "Send a test email to this address with the [mail] settings, then exit")
	resetPassword := flag.String("reset-password", "", "Give the account with this email a new temporary password, then exit")
	showBackupKey := flag.Bool("backup-key", false, "Print the key that opens this server's encrypted backups, then exit")
	openBackup := flag.String("open-backup", "", "Decrypt and unpack an encrypted backup (concord-backup-….tar.gz.age), then exit")
	openTo := flag.String("to", "", "With --open-backup: the new folder to unpack into (default: the backup's name)")
	keyFile := flag.String("key", "", "With --open-backup: the backup key file, or the key itself (default: this server's backup.key)")
	encryptDB := flag.Bool("encrypt-database", false, "Encrypt an unencrypted database (stop the server first), then exit")
	decryptDB := flag.Bool("decrypt-database", false, "Turn an encrypted database back into an unencrypted one (stop the server first), then exit")
	flag.Parse()

	// Opening a backup needs no server settings when the key is given (a
	// restore on a new machine), so it comes before the setup wizard.
	if *openBackup != "" && *keyFile != "" {
		os.Exit(runOpenBackup(*openBackup, *openTo, *keyFile))
	}

	// Parse and initialize logger early
	if *debugMode {
		*logLevel = "debug"
	}
	var level charmlog.Level
	switch *logLevel {
	case "debug":
		level = charmlog.DebugLevel
	case "info":
		level = charmlog.InfoLevel
	case "warn":
		level = charmlog.WarnLevel
	case "error":
		level = charmlog.ErrorLevel
	default:
		level = charmlog.InfoLevel
	}
	server.InitLogger(os.Stderr, level)
	database.SetDebug(level == charmlog.DebugLevel)

	// Detect first-run: no config file specified and default config file absent
	isFirstRun := *configPath == ""
	if isFirstRun {
		if _, err := os.Stat(configFilename); err == nil {
			isFirstRun = false // config file already exists
		}
	}

	// Load configuration
	var config *server.Config
	if isFirstRun {
		// Show ToS first
		if !runToSSetup() {
			fmt.Fprintln(os.Stderr, "\nYou must accept the Terms of Service to run a Concord server.")
			os.Exit(1)
		}

		// ToS accepted, proceed with setup
		config = runFirstRunSetup(nil)
		config.TermsAccepted = true
	} else if *reconfigure {
		// Load existing config to pre-populate setup
		existingConfig := server.DefaultConfig()
		configFileToLoad := configFilename
		if *configPath != "" {
			configFileToLoad = *configPath
		}

		if _, err := os.Stat(configFileToLoad); err == nil {
			if err := loadConfig(configFileToLoad, existingConfig); err != nil {
				fmt.Fprintf(os.Stderr, "Failed to load existing config: %v\n", err)
				os.Exit(1)
			}
		}

		// Run setup with existing values
		fmt.Println("\n🔧 Reconfiguring server settings...")
		config = runFirstRunSetup(existingConfig)
		config.TermsAccepted = existingConfig.TermsAccepted // Preserve ToS acceptance
	} else {
		config = server.DefaultConfig()
		if *configPath != "" {
			if err := loadConfig(*configPath, config); err != nil {
				server.Logger.Fatal("Failed to load config", "error", err)
			}
		} else if _, err := os.Stat(configFilename); err == nil {
			if err := loadConfig(configFilename, config); err != nil {
				server.Logger.Fatal("Failed to load config", "error", err)
			}
		}
	}

	// Apply command line overrides
	if *host != "" {
		config.Host = *host
	}
	if *port != 0 {
		config.Port = *port
	}
	if *dbPath != "" {
		config.DatabasePath = *dbPath
	}

	// The settings file's path (the database key's default home is beside it).
	effectiveConfigPath := configFilename
	if *configPath != "" {
		effectiveConfigPath = *configPath
	}

	// Encrypting or decrypting the database: the server must be stopped.
	if *encryptDB || *decryptDB {
		text, err := server.ConvertDatabase(config, effectiveConfigPath, *encryptDB, askPassphrase)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Println(text)
		return
	}
	// Everything below that opens the database needs its key (dbkey.go).
	if key, info, err := server.ResolveDatabaseKey(config, effectiveConfigPath, askPassphrase); err != nil {
		fmt.Fprintf(os.Stderr, "The database: %v\n", err)
		os.Exit(1)
	} else {
		config.DatabaseKey, config.DatabaseKeyInfo = key, info
	}

	// Backup key tools that exit without starting the server.
	if *showBackupKey {
		key, err := server.BackupKeyText(config.DatabasePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Println("This key opens this server's backups. Keep a copy somewhere safe (a password manager);")
		fmt.Println("without it the backups can't be read. Never share it.")
		fmt.Println()
		fmt.Println(key)
		return
	}
	if *openBackup != "" {
		os.Exit(runOpenBackup(*openBackup, *openTo, server.BackupKeyPath(config.DatabasePath)))
	}

	// One-off account tools that exit without starting the server.
	if *testMail != "" {
		if err := server.SendTestMail(config.Mail, config.ServerName, *testMail); err != nil {
			fmt.Fprintf(os.Stderr, "Test email failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Test email sent to %s. Check its inbox (and spam folder).\n", *testMail)
		return
	}
	if *resetPassword != "" {
		db, err := database.Open(config.DatabasePath, config.DatabaseKey)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to open database: %v\n", err)
			os.Exit(1)
		}
		pw, err := server.ResetPasswordOffline(db, *resetPassword)
		db.Close()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Reset failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("New temporary password for %s: %s\nGive it to them: on the Concord login screen they press Ctrl+F (Forgot password), pick this server, and enter it as the temporary password.\n", *resetPassword, pw)
		return
	}

	// Clear screen for clean server startup (only in normal mode -- either
	// dashboard mode takes over the whole screen itself).
	if !*hybridMode && !*dashboardOnlyMode {
		fmt.Print("\033[2J\033[H")
	}

	// Print beautiful startup banner and information (only in normal mode)
	if !*hybridMode && !*dashboardOnlyMode {
		server.PrintBanner(Version)
	}

	// Handle --admin-email: open DB, grant role, close, then start normally
	if *adminEmail != "" {
		db, err := database.Open(config.DatabasePath, config.DatabaseKey)
		if err != nil {
			server.Logger.Fatal("Failed to open database for admin-email", "error", err)
		}
		if err := db.EnsureAdminRole(*adminEmail); err != nil {
			server.Logger.Fatal("Failed to grant admin role", "email", *adminEmail, "error", err)
		}
		server.AuthLog.Info("Admin role granted", "email", *adminEmail)
		db.Close()
	}

	// Handle --fix-admin: repair broken Admin role
	if *fixAdmin {
		db, err := database.Open(config.DatabasePath, config.DatabaseKey)
		if err != nil {
			server.Logger.Fatal("Failed to open database for fix-admin", "error", err)
		}
		// Get default server
		srv, _, err := db.EnsureDefaultServer("Concord Server")
		if err != nil {
			server.Logger.Fatal("Failed to get server for fix-admin", "error", err)
		}
		if err := db.FixAdminRole(srv.ID); err != nil {
			server.Logger.Fatal("Failed to fix Admin role", "error", err)
		}
		server.AuthLog.Info("Admin role repaired (permissions + is_hoisted + position)")
		db.Close()
	}

	// Handle --cleanup-duplicates: merge and remove duplicate roles
	if *cleanupDuplicates {
		db, err := database.Open(config.DatabasePath, config.DatabaseKey)
		if err != nil {
			server.Logger.Fatal("Failed to open database for cleanup-duplicates", "error", err)
		}
		// Get default server
		srv, _, err := db.EnsureDefaultServer("Concord Server")
		if err != nil {
			server.Logger.Fatal("Failed to get server for cleanup-duplicates", "error", err)
		}
		if err := db.CleanupDuplicateRoles(srv.ID); err != nil {
			server.Logger.Fatal("Failed to cleanup duplicate roles", "error", err)
		}
		server.AuthLog.Info("Duplicate roles cleaned up successfully")
		db.Close()
	}

	// Create and run server
	srv, err := server.New(config)
	if err != nil {
		server.Logger.Fatal("Failed to create server", "error", err)
	}

	// Only after server.New has actually opened the database -- printed
	// earlier, this claimed "Database ✅ Connected" / "ready to accept
	// connections" right before a fatal DB-open failure.
	if !*hybridMode && !*dashboardOnlyMode {
		server.PrintStartupInfo(fmt.Sprintf("%s:%d", config.Host, config.Port), config.DatabasePath)
	}

	// Tell the server where its config lives so Grapevine can persist credentials.
	srv.SetConfigPath(effectiveConfigPath)
	srv.SetBuildInfo(Version, GitCommit, BuildTime)

	// Hybrid wins if both flags are somehow passed together -- it's the
	// more capable of the two (panels plus live scrolling logs), matching
	// Server.Run()'s own precedence.
	if *hybridMode {
		srv.SetDashboardMode(true)
	} else if *dashboardOnlyMode {
		srv.SetFullDashboardMode(true)
	}

	if err := srv.Run(); err != nil {
		server.Logger.Fatal("Server error", "error", err)
	}
	if srv.RestartRequested() {
		// Server Settings › About › Restart: the same program (or, after
		// an update, its replacement at the same path) with the same flags.
		if err := server.Relaunch(); err != nil {
			server.Logger.Fatal("Couldn't start again", "error", err)
		}
	}
}

func loadConfig(path string, config *server.Config) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := toml.Unmarshal(data, config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	return nil
}

// Old printBanner removed - now using server.PrintBanner() with beautiful ASCII art

// runOpenBackup unpacks an encrypted backup with key (a key file, or the
// key itself) into to (default: beside it, named after it).
func runOpenBackup(file, to, key string) int {
	keyText := key
	if data, err := os.ReadFile(key); err == nil {
		keyText = string(data)
	} else if !strings.HasPrefix(strings.TrimSpace(key), "AGE-SECRET-KEY-") {
		fmt.Fprintf(os.Stderr, "Can't read the backup key %s: %v\n", key, err)
		return 1
	}
	if to == "" {
		to = strings.TrimSuffix(file, ".tar.gz.age")
		if to == file {
			to = file + ".opened"
		}
	}
	wrote, err := server.OpenBackup(file, keyText, to)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Couldn't open the backup: %v\n", err)
		return 1
	}
	fmt.Printf("Unpacked %d files into %s:\n", len(wrote), to)
	for _, w := range wrote {
		if !strings.Contains(w, "/") {
			fmt.Println("  " + w)
		}
	}
	fmt.Println("To restore, stop the server and put concord.db (and the other files) back where they came from.")
	return 0
}

// askPassphrase reads a passphrase at the terminal without showing it
// ([database] ask_passphrase); confirm asks twice.
func askPassphrase(prompt string, confirm bool) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return "", fmt.Errorf("the database needs its passphrase, and there's no terminal to type it in (run the server in a terminal, or use a key file: [database] ask_passphrase = false)")
	}
	read := func(p string) (string, error) {
		fmt.Fprint(os.Stderr, p)
		b, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	pass, err := read(prompt)
	if err != nil || !confirm {
		return pass, err
	}
	again, err := read("Again, to be sure: ")
	if err != nil {
		return "", err
	}
	if again != pass {
		return "", fmt.Errorf("the two passphrases don't match")
	}
	return pass, nil
}
