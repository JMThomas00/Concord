# What Concord protects, and what it doesn't

Concord is built so people can run their own chat, owned by them and not by a company. That doesn't make it a safe place for everything. This page says plainly what each protection does, what it doesn't, and what nothing in Concord can protect.

If your safety depends on this, read all of it. Concord hasn't had an independent security audit.

## The short version

- **The person who runs a server can read everything on it**: every message, who sent it, and when. Choose servers whose admins you trust.
- **Tor hides where you are and where the server is.** It doesn't hide what you say, your name, or how you write.
- **Encryption at rest protects copies**: a stolen disk, a copied folder, a backup. A server that's running can read its own database, and so can whoever takes it over.
- **Your own computer is part of it.** Anyone who can open your Concord folder can sign in as you.

## Who can see what

| Who | Without Tor | With Tor |
|---|---|---|
| **The server's admin** | Your messages, your account's email, when you're online, your IP address | Your messages, your email, when you're online; **not** your IP address |
| **Someone watching the network** (your ISP, the café's Wi-Fi, a government) | On a normal server without https: your messages. Always: that you connect to that server | That you use Tor (with a bridge, not even that). Not which server, not your messages |
| **Other members** | What you post, your name, when you're online. In a call or a file transfer: your IP address | What you post, your name, when you're online. Not your IP address: over Tor, voice is off and files go through the server |
| **Someone who learns the server's .onion address** | — | Nothing, without the access key from an invite |
| **Someone who takes the server's disk or a backup** | With database encryption: nothing from the database, unless the key was taken too (below) | The same |

## Tor

**What it does**

- **A server on Tor** gets a `.onion` address. Nobody can tell where in the world it is or who runs it from the address, and it needs no port forwarding, domain or certificate.
- **The access key** (on by default) means a server can't be found or reached at all without the key that invites carry. Knowing the address isn't enough.
- **You on Tor:** servers see Tor, not your IP address. People watching your connection see that you use Tor, but not which servers. With a **bridge** (Snowflake or obfs4), they don't easily see that you use Tor either.
- Connections to a `.onion` address are encrypted by Tor itself, end to end between you and the server.

**What it doesn't**

- **It doesn't hide what you write.** Your name, the way you write, the times you're online, the people you talk to and the things you mention can all identify you. People have been identified from their writing style alone.
- **It doesn't hide anything from the server's admin** except your IP address.
- **Anyone holding an invite can reach the server.** If an invite leaks, the admin should make a new key (**T** twice on Server Settings › About). The old one keeps working for a week while members pick up the new one, so it isn't instant.
- **"Where needed" mode** (the default) uses Tor only for `.onion` servers and servers you switch to it. Everything else (other servers, the Grapevine, update checks) connects directly and shows your IP address. Choose **Everything** in Settings › Network if that matters.
- **Voice is off over Tor.** Voice connects computers directly, which would show your address. Files go through the server instead, so the server's admin can see them pass, though it doesn't keep them.
- **The built-in bridges are shared by many people** and can be slow or blocked. Your own bridge lines (from bridges.torproject.org, @GetBridgesBot on Telegram, or bridges@torproject.org) work better.
- **A total internet shutdown stops Concord**, like anything else that needs the internet.
- **The server's onion key is its identity.** It lives in the server's `tor/` folder and in its backups. Whoever has it can pretend to be the server.

## Encrypted database

**What it does**

- New servers keep their database encrypted on disk. A copied data folder, a stolen disk or an old hard drive shows nothing readable without the key.
- **Backups are encrypted** with a separate backup key, so a backup uploaded somewhere else can't be read there.

**What it doesn't**

- **A key file on the same machine doesn't protect a seized server.** By default the server reads its key from a file so it can restart by itself. Whoever takes the whole machine has the key too, whether the machine is on or off.
  - The installer and Docker keep the key outside the data folder, so copying that folder alone isn't enough. A server set up by hand keeps it beside its settings file, which may be the same folder.
  - Only **a passphrase typed at every start** (`ask_passphrase = true`) keeps a seized machine that is switched off unreadable. The cost: the server can't restart by itself after a crash, a reboot or an update.
- **A running server can read its own database.** While it's on, the key is in its memory. Someone who takes over the running machine, or makes the admin hand it over, can read everything.
- **There's no tamper detection.** The encryption (Adiantum) hides the contents but can't tell if someone changed the file. A change garbles the part that was changed rather than writing anything readable, but Concord may not notice it happened.
- **Copies taken over time give something away.** The database is encrypted in 4 KiB blocks. Someone who has several copies from different days can see which blocks changed, and so roughly how much and where activity happened, without reading any of it. The file's size shows how big the server is, too.
- **The backup key sits beside the database.** It's for opening backups stored elsewhere. Anyone with the server's data folder has the backups' key as well.

## Things nothing in Concord protects

- **What people do with what you send.** Members can copy, screenshot or forward anything.
- **Your own computer.** `~/.concord` holds your sign-in tokens and the access keys for Tor servers, unencrypted. Anyone who can open it can sign in as you and find those servers. Lock your computer and use disk encryption.
- **Plugins.** A plugin is a program the server runs. It sees what's posted in its channels, and some send it elsewhere: an AI plugin sends messages to its AI provider.
- **Email.** Verification codes and emailed invites go through the email providers on both ends. An emailed Tor invite carries the server's access key.
- **The Grapevine.** A listed server is public by choice, and a hub sees the IP address of anyone who browses it or joins from a listing (unless they use Tor for everything).
- **Normal servers without https.** On a plain `address:port`, messages cross the network unencrypted. A server reached through https (like the official one, behind Cloudflare) is encrypted to that service, which can read the traffic at its end.
- **Voice and file transfers without Tor** are encrypted, but connect your computer to the other person's, so each sees the other's IP address. A STUN server (Google's by default) also sees your address when a call starts.
- **Messages after they're deleted.** Deleting hides a message from members; the server keeps it until its message retention (Server Settings › Messages) removes it, and a backup taken earlier still has it.

## Still to come

- **TLS for normal servers**, so messages on a plain address are encrypted too.
- **End-to-end encrypted direct messages**, which even the server's admin can't read.

When those arrive, this page will change with them.
