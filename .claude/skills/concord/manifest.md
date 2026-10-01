# `plugin.toml`

Concord reads the manifest to install, run and configure a plugin. It ships
at the root of the release zip, next to the binary. Admins never edit it;
its fields become forms in the client.

```toml
[plugin]
id = "concord-dice"            # unique on the server; also the folder name. [a-z0-9-]
name = "Dice"                  # shown in Settings > Plugins
version = "0.1.0"              # release.go stamps the git tag here
description = "Rolls dice."
author = "you"
source_url = "https://github.com/you/concord-dice"   # U (update) with nothing typed uses this
# instances = true             # admins can run several named copies (personas) of this one install

[process]
restart_on_crash = true
max_restarts = 5               # consecutive crashes before giving up
# restart_backoff_seconds = 2  # doubles per crash, up to 60s
# startup_timeout_seconds = 20 # an update that doesn't connect in time rolls back
# [process.env]                # non-secret constants only; a plugin gets few server env vars
# LOG_LEVEL = "info"

[process.entrypoint.linux]
bin = "concord-dice"
[process.entrypoint.darwin]
bin = "concord-dice"
[process.entrypoint.windows]
bin = "concord-dice.exe"
# args = ["--flag"]            # optional, per OS

[[channel_kind]]
kind = "dice"                  # unique within the plugin
display_name = "Dice"          # the channel type admins pick
icon = "⚄"
remote_pane = false            # false: chat relay channel; true: a pane the plugin draws

  [[channel_kind.create_field]]     # per-channel option, set when creating the channel
  key = "sides"
  label = "Default die"
  type = "select"
  options = ["6", "20", "100"]
  default = "6"
  help = "Used when someone types just !roll"

[[server_config_field]]             # server-wide setting (Settings > Plugins > Enter)
key = "api_key"
label = "API key"
type = "secret"
help = "From your account page at example.com"
```

## Field types

| `type` | Value the plugin receives | Notes |
|---|---|---|
| `text` | the string | |
| `number` | a decimal string | validated as a number |
| `boolean` | `"true"` / `"false"` | |
| `select` | one of `options` | |
| `channel_select` | a channel **ID** (UUID string) | shown to admins as `#name` |
| `channel_multi_select` | comma-separated channel IDs, `""` for none | a checklist of text channels; treat empty as "every channel" if that suits the setting |
| `secret` | the plaintext, to the plugin only | **server fields only**; encrypted at rest; admins see only "set" |

Every field takes `key`, `label`, `type`, optional `default`, `required`,
`help` (one line under the field: always write one) and, for `select`,
`options`. Concord validates values against the manifest before saving and
shows errors per field, so the plugin can trust types and options. It should
still validate meaning (a sensible range, say) and fall back to defaults.

Where values arrive:

- `server_config_field` → `OnConfig`: `info.ConfigValues[key]`, on connect
  and on every save. Apply changes live; never require a restart.
- `create_field` → `OnChannel`: `ch.PluginConfig[key]` for that channel,
  on connect and when the channel is created or edited. The table kit passes
  them to `Rules.New` as options.

## Validation rules that reject a manifest

- Missing `id`, `name` or `version`, or no entrypoint for the server's OS.
- Duplicate `kind`s, or duplicate field `key`s within one list.
- An unknown field `type`, or a `secret` in a `create_field` (channel
  settings are visible to members).
- The zip's binary missing, or `id` not matching on update.

Admins see channel settings behind a **Configure…** row on the channel form,
and server settings on the plugin's page (Settings > Plugins > Enter). So
long lists of either are fine.

## Instances (several personas from one install)

Set `[plugin] instances = true` when it makes sense to run the same plugin
more than once on a server under different names. Mynah's AI personas are
the example. An admin adds, renames and removes instances on the plugin's
page, with no extra download. Each instance:

- runs as its own process, from the same folder and binary;
- has its own service account (shown by the instance's name), settings,
  channels and `CONCORD_PLUGIN_DATA_DIR`;
- gets its name as `info.Name` in `OnConfig`. Use it, not a hard-coded
  name, wherever the plugin refers to itself (an @mention trigger, say).

The channel form offers the plugin once, and its Configure page asks which
instance owns the channel. Updating the plugin restarts every instance.
Write the plugin as if it were the only copy: nothing in the SDK changes.

**Built-in @mention relay:** Concord forwards a message from a channel the
plugin doesn't own when the plugin has a `mention_enabled` server field set
to `"true"` and the message @mentions its `mention_trigger` value (an empty
trigger defaults to the plugin's or instance's name). An optional
`mention_channels` field (`channel_multi_select`) limits that relay to the
channels listed, and empty means everywhere. Declare those three keys to
get it.
