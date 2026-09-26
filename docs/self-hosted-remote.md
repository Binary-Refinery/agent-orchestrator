# Self-hosted remote hosts (experimental)

An AO session belongs to the daemon that started it. A desktop or phone is a
client of that daemon; closing or switching clients does not move the session.
Each client saves its own host connections. There is no shared host directory
or new global database. The existing AO Cloud path is separate.

## Connect a machine

1. Run the AO daemon on the machine that will own the sessions (`ao daemon`).
   Use your OS service manager if it must survive logout/reboot.
2. Run `ao remote-host enable` **on that machine**. It prints its stable Host ID,
   reachable addresses, and connection password. `ao remote-host status` shows
   the current details; `ao remote-host disable` closes the network listener.
3. On each desktop client, enable **Settings → General → Remote hosts
   (experimental)** and add the address/password. Repeat for as many remote
   machines as needed. The sidebar lists each machine separately; **Start on
   [machine]** creates the worker on that machine.
4. On mobile, pair the machine in **Settings → Machines**. Switch the selected
   machine there to view and continue its sessions. Pair the same machine on a
   second laptop to continue the same host-owned session.

The daemon's normal unauthenticated listener remains on `127.0.0.1`. The
opt-in remote listener is password-protected but plain HTTP, intended only for
a trusted private network or an encrypted tunnel. Do not expose it directly
to the public internet. Clients remember a stable daemon-installation ID and
reject an address that later answers as a different host. This protects
against accidental address reassignment, not an active network attacker or a
copied AO data directory. Desktop connection passwords live in
`~/.ao/remotes.json` (or `AO_DATA_DIR/remotes.json`) with owner-only permissions.

The first desktop remote surface supports starting workers, Chat messages,
terminal attach, and stopping sessions. Native editor, files, browser, and
approval controls are not exposed in that remote view yet. Push notifications
carry the owning host ID; a tap for a different selected host opens the board
instead of acting on a same-ID session there. Older pushes without a host ID
also open the board. AO Cloud placement remains its existing separate flow;
this slice does not unify all three placements into one picker.
