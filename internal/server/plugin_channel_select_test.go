package server

import (
	"path/filepath"
	"testing"

	"github.com/concord-chat/concord/internal/database"
	"github.com/concord-chat/concord/internal/models"
	"github.com/concord-chat/concord/internal/plugins"
)

func TestMigrateChannelSelectNamesConvertsNamesToIDs(t *testing.T) {
	db, err := database.New(filepath.Join(t.TempDir(), "concord.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	srv, _, err := db.EnsureDefaultServer("Test")
	if err != nil {
		t.Fatal(err)
	}
	activity := models.NewTextChannel(srv.ID, "plugin-activity")
	if err := db.CreateChannel(activity); err != nil {
		t.Fatal(err)
	}

	reg, errs := plugins.Discover(filepath.Join("..", "plugins", "testdata"))
	if len(errs) != 0 {
		t.Fatalf("Discover: %v", errs)
	}

	// A value saved by the old client: the channel's name.
	if err := db.SetPluginServerConfig("HelloPlugin", "activity_notify_channel", "plugin-activity"); err != nil {
		t.Fatal(err)
	}
	migrateChannelSelectNames(db, reg)
	got, _ := db.GetPluginServerConfig("HelloPlugin")
	if got["activity_notify_channel"] != activity.ID.String() {
		t.Fatalf("activity_notify_channel = %q, want the channel's ID %s", got["activity_notify_channel"], activity.ID)
	}

	// Already an ID: untouched. Ambiguous or unknown names: left for the admin.
	migrateChannelSelectNames(db, reg)
	if got, _ := db.GetPluginServerConfig("HelloPlugin"); got["activity_notify_channel"] != activity.ID.String() {
		t.Fatalf("second run changed an ID value to %q", got["activity_notify_channel"])
	}
	if err := db.SetPluginServerConfig("HelloPlugin", "activity_notify_channel", "no-such-channel"); err != nil {
		t.Fatal(err)
	}
	migrateChannelSelectNames(db, reg)
	if got, _ := db.GetPluginServerConfig("HelloPlugin"); got["activity_notify_channel"] != "no-such-channel" {
		t.Fatalf("unresolvable name was rewritten to %q", got["activity_notify_channel"])
	}
}
