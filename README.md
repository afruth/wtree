# wtree

`wtree` is a terminal tool for Git worktrees and the Docker Compose stacks that belong to them. It shows them in one tree, on one screen, from a large monitor to a phone.

- See each worktree with its branch and its stacks, for one repo or for the whole machine.
- See CPU, memory, and disk usage for each stack.
- Create a worktree at the current HEAD. Open a worktree in a new tab, or jump to the tab that already has it open.
- Delete or stop one entry or many. `wtree` shows what each delete costs before it runs, and it refuses what it must not take.
- Find what you can clean up, with the reason for each entry.
- Use all of it on a phone: tap, or use plain keys. No action needs `ctrl`, or a long press.

`wtree` has two parts. `wtree` is one Bash script, and it holds all the rules. `wtree-tui` is the full-screen program, and it shows what the script says. If `wtree-tui` is not there, the script runs a smaller UI in [`fzf`](#fallback-to-fzf). `wtree` also installs as a plugin for [Rex](https://www.superlogical.com/rex).

## Install

```sh
curl -fsSL https://raw.githubusercontent.com/afruth/wtree/main/install.sh | sh
```

The installer puts `wtree` and `wtree-tui` of the latest release in `~/.local/bin`, and it verifies their checksums. For each host, it picks Linux or macOS, on amd64 or arm64.

| Setting | Result |
| - | - |
| `WTREE_BIN_DIR=DIR` | Install in this directory |
| `WTREE_VERSION=v0.5.0` | Install this release |
| `sh -s -- --no-tui` | Install only the script, and remove an old `wtree-tui` |

If the installer cannot get a good `wtree-tui` for your host, it says why, it removes an old one, and it installs only the script. It never installs the script of a release unverified: if the checksum is wrong, if the checksums cannot be read, or if the host has no `sha256sum` or `shasum`, it installs nothing and changes nothing. If it cannot read which release is the latest, it stops too. Only a repo with no release at all gets the script of the main branch, and the installer says that this script is not verified.

You need `bash` and git 2.31 or later. Stacks need Docker Compose v2. The fzf UI needs `fzf` 0.53 or later.

To build from a checkout, you need Go 1.26 and `make install`. It installs both files in `~/.local/bin`, or in `WTREE_BIN_DIR`.

## Use

| Command | Result |
| - | - |
| `wtree` | Show the worktrees and stacks of this repo. Outside a repo, `wtree-tui` opens on this machine, and the `fzf` UI stops with an error |
| `wtree -d` | Start with a flat list of the stacks. Press `o` to go on to the tree |
| `wtree -g` | Show all the worktrees and stacks on this machine, in any directory |
| `wtree -g -d` | Start with a flat list of all the stacks on this machine |
| `wtree -n` | Create a worktree in `../worktrees/<unique-name>` at the current HEAD, with a detached HEAD, and open it. It needs no UI |
| `wtree --nuke` | Delete all that goes without force, after one summary and one confirmation. See [Nuke](#nuke) |
| `wtree --nuke -c` | Delete only what the cleanup view calls `safe` |
| `wtree --nuke -a` | Delete all that force can take, with the volumes |
| `wtree --nuke -g` | The same for the whole machine. It goes with `-c` and `-a` |
| `wtree -h` | Show the help |

The `../worktrees` directory is beside the main checkout.

Git has no list of the repos on a machine, so machine scope looks for them in your home directory. It does not look in directories such as `node_modules`, `.cache`, `.local`, and `.config`, or on other file systems. The result stays in `~/.cache/wtree` and is updated in the background when it is older than 30 minutes. To look in other directories, set `WTREE_ROOTS`, with a colon between the directories:

```sh
WTREE_ROOTS=$HOME/code:/srv/repos wtree -g
```

The worktrees of a repo with a stack always appear, even when the repo is outside these directories.

### Nuke

`wtree --nuke` deletes many entries with one confirmation. It reads the scope, chooses the targets, and shows one summary: what goes, what stays and why, and what is lost. After your yes it runs to the end, and it asks nothing more. A step that fails does not stop the others.

| Level | Worktrees | Their stacks | Stacks without a worktree | Confirm |
| - | - | - | - | - |
| `-c` | The `safe` rows of the cleanup view | Deleted. A `safe` row has no volumes | The `safe` rows | `y` |
| none | Each one that a delete takes without force | Deleted, the volumes stay | Stacks whose worktree is gone. The volumes stay | `y` |
| `-a` | Each one that force can take | Deleted with their volumes | Stacks whose worktree is gone, and `volumes only` stacks, with their volumes | Type `nuke` |

The rules are those of a delete from the list. No level takes the main checkout, the worktree that you are in, a locked worktree, or a stack of a worktree that stays. In machine scope, stacks in directories outside a repo stay too. A worktree that shares a stack with a worktree that stays also stays.

`wtree --nuke -a` also deletes a git repo inside a worktree, with its commits. The summary names it, and your typed `nuke` confirms it.

Each plan is read again just before it runs. If something changed after the summary, that part does not run. Run `wtree --nuke` again to see the new state.

## Keys

All the keys are plain letters. To search, press `/`. Press `?` to see the keys that apply now, and why a key does nothing now.

| Key | Action |
| - | - |
| `j` `k`, arrows | Move |
| `J` `K`, `PgUp` `PgDn` | Move by a page |
| `h` `l`, left and right arrows | Collapse, expand |
| `z` | Collapse or expand all |
| `enter` | Open the entry, or jump to the tab that has it open |
| `a` | Open the actions of the row |
| `space`, `tab` | Mark the row, or remove the mark |
| `A` | Mark all the rows that the search shows |
| `u` | Remove all the marks |
| `s` | Stop |
| `d` | Delete. You choose the level in the next sheet |
| `n` | Create a worktree |
| `g` | Change the scope: repo or machine |
| `c` | Show what you can clean up |
| `/` | Search. `enter` applies it, and `backspace` on an empty field clears it |
| `o` | Change the order: tree, disk, cpu, name |
| `p` | Show or hide the detail panel |
| `r` | Read everything again |
| `L` | Show the log of the last batch |
| `y` | Copy the path of the row |
| `?` | Help |
| `q`, `esc` | Quit, or close what is open |
| `backspace` | Go back: clear the search, leave the cleanup view, then clear the marks. It does not quit, because it repeats while it is held |

In a sheet, `1` to `9` pick an item, and `0` cancels.

A search matches text, without approximate matches. Terms are separated by spaces, and `-term` excludes a term. The search also has the fields `is:`, `state:`, `repo:`, and `branch:`. A field with an unknown name or value matches nothing.

The keys that act on rows do nothing until the structure of the list has loaded. Typing still works, and you can always quit.

A plan can take a while. The status line says when `wtree` reads one, and any other key cancels it, so a sheet never opens over what you do next. A sheet ignores `y`, `enter` and a tap on confirm until it has held still for a moment, so a held key cannot answer it. `s` stops the row under the cursor at once. Marked rows, and a tap on stop, get a sheet first.

## Touch

- A tap on a row moves the cursor to it. It does nothing else.
- A tap on the row of the cursor opens the actions of that row.
- A tap on the left edge of a row marks it, and a tap on the expand mark beside that opens or closes it. From 60 columns, the first two cells mark and the next two expand. Below 60 columns a row has two lines: in its first three cells, the first line expands and the second line marks.
- The bar at the bottom has buttons for the main actions. Each button shows its key.
- A tap on an action that is not available shows why.

A tap never deletes and never opens a tab. No action depends on a double tap, a long press, or the time between taps, and each one has a plain key, so a phone client that sends no taps can still use all of it.

The layout follows the size of the terminal. Below 60 columns, each row uses two lines, and the sheets fill the width.

## What a delete can take

The script makes the rules, and each UI shows what the script says. Before a delete, read the sheet: it lists what goes, what stays and why, and what the delete costs. A delete checks again when it runs, so a change in the meantime stops it.

A delete does not take the main checkout, the worktree that you are in, a locked worktree, or a worktree with another worktree inside it. Force does not take them either. A locked worktree shows the command that unlocks it.

Without force, a delete also keeps a worktree with changed files (also files that `git status` does not check, because of a config or an assume-unchanged or skip-worktree flag), a detached worktree with commits that no branch or tag holds, and a worktree with another git repo inside it. Force takes these. For another git repo inside, you must type the name of the worktree. A bare repo counts as another repo, and so does a repo with its own commits that the worktree tracks as a gitlink. A submodule does not count.

CAUTION: Each delete of a worktree also deletes ignored files in it, such as `.env` and `node_modules`. Force also deletes changed files and commits that no branch or tag holds.

If the directory of a worktree is gone, a delete removes the entry of that worktree only. If that entry is detached and holds the only copy of a commit, only force removes it.

A delete of a worktree can also offer its stacks, and a delete of a stack can offer its worktree. The sheet shows these as levels, and the mildest level is the default. A worktree stays if another stack that you did not select has containers in it.

CAUTION: A delete with volumes deletes the data in the volumes. You must type a text in these cases:

- You selected more than one stack. Type the number of stacks, such as `3 stacks`.
- The stack belongs to a main checkout, or to a directory that is not in a repo. A directory can be reached through a symlink. Type the stack name, or the number of stacks if you selected more than one.
- The directory of the stack is gone, or the stack has no containers, so `wtree` cannot tell where it belongs. This includes a `volumes only` row.
- From the worktree side, the stack also has containers outside the selected worktrees. The sheet names these directories.

The sheet shows the text. `wtree-tui` ignores case, spaces around the text, and a full stop at the end, because a phone keyboard can add them. It never fills in the text for you. In the fzf UI, type the text exactly.

The batch continues after an error, and it then shows the errors. If a worktree cannot be deleted, `wtree` does not delete its stacks. If a stack or one of its volumes cannot be deleted, `wtree` marks the stack as failed, keeps its other volumes, and does not delete its worktree. Failed entries stay marked, so you can try again.

### Cleanup

Press `c` to see what you can clean up, and how much disk it holds.

| Group | Entries |
| - | - |
| `safe` | A merged worktree with no stacks, or with all its stacks stopped for 7 days or more, and no volumes. A stack with no volumes, whose directory is gone and whose name holds the name of the repo |
| `needs a look` | Changed files, commits that nothing holds, a gone upstream, a stack that still runs from a merged worktree, a stale stack, a stack with volumes, a `volumes only` stack, a gone stack whose name does not hold the name of the repo, or data that Docker did not give |
| `never` | The main checkout, the worktree that you are in, a locked worktree |

`safe` means that `wtree` expects no refusal. It does not skip the sheet, and a delete can still refuse the entry. Volumes can hold the only copy of their data, so an entry with volumes is never `safe`.

### Which stacks appear

- In machine scope, all stacks appear. A stack shows `worktree gone` if its directory no longer exists. The other rules in this list apply only in repo scope.
- A stack appears if its directory is in the repo or in one of its worktrees.
- A stack shows `worktree gone` if its directory is no longer in a Git repo and the repo name is in its stack name or path. Read the name before you delete it.
- A stack shows `volumes only` if it has no containers and its name starts with the repo name.
- Containers that Docker Compose did not start do not appear.

CPU and memory are live. Disk data comes from `docker system df`, which can take minutes on a large engine, so a background job keeps it in `~/.cache/wtree`. It runs when the data is older than 30 minutes, or after a delete.

If Docker does not answer, `wtree` does not hang. The list still shows the worktrees, and it says what is missing. If the volumes cannot be read, a delete with volumes stops before it deletes anything. A stop or a delete has no time limit, because it can need a long time on a large stack.

## Tabs

`wtree` finds the terminal that it runs in. It opens a tab in that terminal, or jumps to a tab that already has the worktree open. If you are already in that worktree, `wtree` tells you and opens nothing.

| Terminal | New tab | Jump to an open tab | You must set |
| - | - | - | - |
| Rex | Yes | Yes | Remote Control on |
| herdr | Yes | Yes, across workspaces | - |
| cmux | Yes, a new workspace | Yes, across workspaces | - |
| tmux | Yes | Yes | - |
| zellij | Yes | Yes, zellij 0.44 or later | - |
| WezTerm | Yes | Yes | - |
| kitty | Yes | Yes | `allow_remote_control yes` in `kitty.conf` |
| Ghostty on macOS | Yes | Yes | Ghostty 1.3 or later, and the macOS Automation permission |
| iTerm2 | Yes | Yes | The macOS Automation permission |
| Terminal on macOS | Yes, with the macOS Accessibility permission, else a new window | Yes | The macOS Automation permission |
| Other terminals | No, `wtree` prints the path | No | - |

A multiplexer takes priority over the terminal that it runs in. For example, `wtree` in tmux in Ghostty opens a tmux window.

## Rex plugin

1. Clone the repo into the Rex configuration directory:

   ```sh
   git clone https://github.com/afruth/wtree ~/.config/rex/wtree
   ```

2. Add these lines to `~/.config/rex/init.lua`:

   ```lua
   require("wtree").setup{}
   rex.bind("ctrl+space>w", "wtree")
   rex.bind("ctrl+space>n", "wtree_new")
   rex.bind("ctrl+space>d", "wtree_docker")
   rex.bind("ctrl+space>g", "wtree_global")
   rex.bind("ctrl+space>s", "wtree_docker_global")
   rex.bind("ctrl+space>ctrl+space", "pane.send_key", { key = "ctrl+space" })
   ```

3. Turn on Remote Control in Rex > Settings > Rex Server. `wtree` needs it to jump to an open tab.

4. Build `wtree-tui` beside the script of the plugin. You need Go. Without this step, the float uses `fzf`, or a `wtree-tui` on `PATH`:

   ```sh
   make -C ~/.config/rex/wtree install WTREE_BIN_DIR="$HOME/.config/rex/wtree/bin"
   ```

5. Reload the configuration:

   ```sh
   rex config reload
   ```

The keys open `wtree` in a float, in the directory of the focused terminal. The actions are also in the command palette under "wtree". The float always starts the script, and the script finds `wtree-tui` beside itself or on `PATH`, and else uses `fzf`. `git`, and `fzf` if you need it, must be on the `PATH` that your login shell sets. `wtree-tui` also reports its progress to the tab of the float.

The float takes 80% of the window. If the terminal has fewer than 70 columns, it takes all of the window. To change this, use `require("wtree").setup{ bounds = { 0.1, 0.1, 0.8, 0.8 }, narrow_bounds = { 0, 0, 1, 1 }, narrow_columns = 70 }`. The values of the bounds are x, y, width, and height, as fractions of the window. If you set only `bounds`, it applies to each terminal.

### Other hosts and devices

The Rex app can run the plugin from its own home configuration, also for a session on another host. Clone the repo to the same path under your home directory on each host, and keep the clones at the same version. A script under `$HOME` is then found on the host of the float, and it looks for `wtree-tui` on that host. A `script` path outside your home directory must exist on that host.

Rex has one focus for each session, and no API gives the focus of one device. When two devices share a session, `wtree` uses the focused block that the calling device owns. If it cannot tell, it uses the last focus of the session, which can be the pane of the other device. The header of the list, or the prompt in `fzf`, shows the name of the repo. Check it before you act on the list.

If the tab already has a `wtree` float of this device, the key focuses that float. It does not open a new one, and it never closes a float, because a float can be in the middle of a delete. To open a different list, press `esc` in the float first. A float of another device stays behind the new float. Open floats are found by their labels, so this still works after `rex config reload`.

To upgrade the plugin, pull and build again. A `git pull` does not change `wtree-tui`. If the old program does not match the new script, the float uses `fzf` until you build it:

```sh
git -C ~/.config/rex/wtree pull
make -C ~/.config/rex/wtree install WTREE_BIN_DIR="$HOME/.config/rex/wtree/bin"
rex config reload
```

## Fallback to fzf

`wtree` chooses its UI each time it starts. It uses `wtree-tui` if all of these are true, and else it uses `fzf`:

- `wtree-tui` is beside the script, also through a symlink to the script, or it is on `PATH`.
- `wtree-tui` runs on this host, and it knows the contract of this script. If not, `wtree` prints one line and uses `fzf`.
- The standard output is a terminal.

| Setting | Result |
| - | - |
| `wtree --tui`, `WTREE_UI=tui` | Use `wtree-tui`, also with no terminal. With no `wtree-tui`, stop with an error |
| `wtree --fzf`, `WTREE_UI=fzf` | Use `fzf` |
| `WTREE_TUI=PATH` | Use this `wtree-tui` |

A flag wins over `WTREE_UI`.

### What still needs fzf

Nothing needs `fzf` if `wtree-tui` works on your host. These cases need `fzf` 0.53 or later:

- A host with no `wtree-tui`, such as a platform with no release, or an install with `--no-tui`.
- A `wtree-tui` from another release that does not know the contract of the script.
- A list with no terminal on the standard output.
- `wtree --fzf`.

`wtree -n` and `wtree core` never need a UI. Some Linux package managers ship an older `fzf`. For example, Ubuntu 24.04 ships `fzf` 0.44. If `wtree` reports this, install a newer one from the [fzf releases](https://github.com/junegunn/fzf/releases).

### Keys in the fzf UI

The fzf UI has one list for worktrees and one for stacks. `wtree` opens the worktrees, and `wtree -d` opens the stacks. It uses the same rules for a delete, in prompts. There is no cleanup view.

| Key | Worktree list | Stack list |
| - | - | - |
| `enter` | Open the worktree, or jump to its tab | Open the directory of the stack, or jump to its tab |
| `tab` | Select the entry | Select the entry |
| `ctrl-a` | Select all the matches | Select all the matches |
| `ctrl-s` | - | Stop |
| `ctrl-d` | Delete | Delete the containers and networks, and keep the volumes |
| `ctrl-f` | Force delete | Delete the stack and its volumes |
| `ctrl-r` | Reload. With `-g`, also look for repos again | Reload |
| `esc` | Quit | Quit |

The keys act on the selected entries, or on the current entry if no entry is selected. `fzf` search finds approximate matches, so `ctrl-a` can select more than you expect. Read the list before you confirm. While a list loads, the keys that act on rows do nothing. A reload clears the selection. `wtree` ignores a key that you press before a prompt appears.

Below 60 columns, each entry uses more lines, and `enter` or a tap opens a menu of the actions. To select several entries without a `tab` key, choose `select several` in the menu, tap the entries, and press `enter`.

## Credits

The structure of the Rex plugin follows [rex-worktrunk](https://github.com/haxybaxy/rex-worktrunk) (MIT).

## License

MIT
