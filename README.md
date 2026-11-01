# wtree

`wtree` is a small terminal tool for Git worktrees and the Docker Compose stacks that belong to them. It works on one screen, from a large monitor to a phone.

- Create a worktree at the current HEAD with one command.
- Search all worktrees of a repo, or all worktrees on the machine. Open a worktree in a new tab, or jump to the tab that already has it open.
- See which Docker stacks belong to each worktree, and which branch each stack belongs to.
- See CPU, memory, and disk usage for each stack.
- Delete a worktree and get an offer to delete its stacks. Delete a stack and get an offer to delete its worktree.
- Select several worktrees or stacks, confirm once, and follow the progress.
- Use `wtree` on a phone screen: the list changes its layout, and a menu replaces the `ctrl` keys.

`wtree` is one Bash script. It also installs as a plugin for [Rex](https://www.superlogical.com/rex).

## Install

You need `bash`, git 2.31 or later, and `fzf` 0.53 or later. `wtree -d` also needs Docker Compose v2.

```sh
curl -fsSL https://raw.githubusercontent.com/afruth/wtree/main/install.sh | sh
```

The installer puts the script in `~/.local/bin`. To use a different directory, set `WTREE_BIN_DIR`.

Some Linux package managers ship an older `fzf`. For example, Ubuntu 24.04 ships `fzf` 0.44. If `wtree` reports this, install a newer `fzf` from the [fzf releases](https://github.com/junegunn/fzf/releases).

## Use

| Command | Result |
| - | - |
| `wtree` | Search the worktrees of this repo |
| `wtree -n` | Create a worktree in `../worktrees/<unique-name>` at the current HEAD, with a detached HEAD, and open it |
| `wtree -d` | Search the Docker Compose stacks of this repo and its worktrees |
| `wtree -g` | Search all the worktrees on this machine |
| `wtree -g -d` | Search all the Docker Compose stacks on this machine |
| `wtree -h` | Show the help |

The `../worktrees` directory is beside the main checkout.

### All worktrees and stacks

`wtree -g` and `wtree -g -d` do not filter by repo, and they work in any directory. `-n` does not work with `-g`, because a new worktree needs a repo. Each entry shows the name of its repo, so you can search by repo.

Git has no list of the repos on a machine, so `wtree -g` looks for them in your home directory. It does not look in directories such as `node_modules`, `.cache`, `.local`, and `.config`, or on other file systems. The result stays in `~/.cache/wtree` and is updated in the background when it is older than 30 minutes. Press `ctrl-r` to look again immediately.

To look in other directories, set `WTREE_ROOTS` and press `ctrl-r`. Separate the directories with a colon:

```sh
WTREE_ROOTS=$HOME/code:/srv/repos wtree -g
```

The worktrees of a repo with a stack always appear, even when the repo is outside these directories.

### Select several entries

| Key | Action |
| - | - |
| `tab` | Select the entry, or remove it from the selection |
| `ctrl-a` | Select all the entries that match the search, or remove them from the selection |

The keys in the next sections act on all the selected entries. If no entry is selected, they act on the current entry. `wtree` asks for one confirmation, shows a progress bar, and continues after an error. It then shows the errors. If a worktree cannot be deleted, `wtree` does not delete its stacks. If a stack cannot be deleted, `wtree` does not delete its worktree.

### Keys in the worktree list

| Key | Action |
| - | - |
| `enter` | Open the worktree, or jump to the tab that has it open |
| `ctrl-d` | Delete the worktree |
| `ctrl-f` | Force delete the worktree |
| `ctrl-r` | Reload the list. With `-g`, also look for repos again |
| `esc` | Quit |

`ctrl-d` does not delete the main checkout, the worktree that you are in, a worktree with changed files, or a detached worktree with commits that no branch or tag holds. `ctrl-f` does not delete the main checkout or the worktree that you are in. The confirmation shows the entries that stay and why.

If the worktrees have Docker stacks, the confirmation also asks whether to delete the stacks and their volumes.

CAUTION: Both delete keys also delete ignored files in the worktree, such as `.env` and `node_modules`. `ctrl-f` also deletes changed files and commits that no branch or tag holds.

### Keys in the stack list

| Key | Action |
| - | - |
| `enter` | Open the directory of the stack, or jump to the tab that has it open |
| `ctrl-s` | Stop the stack |
| `ctrl-d` | Delete the containers and networks, and keep the volumes |
| `ctrl-f` | Delete the stack and its volumes |
| `ctrl-r` | Reload the list |
| `esc` | Quit |

`ctrl-d` and `ctrl-f` do not delete a worktree unless you confirm this. A worktree with changed files stays.

CAUTION: `ctrl-f` deletes the data in the volumes. You must type a text to confirm in these cases:

- You selected more than one stack. Type the number of stacks, such as `3 stacks`.
- The stack belongs to a main checkout, or to a directory that is not in a repo. Type the stack name, or the number of stacks if you selected more than one.

Before you confirm, read the list of stacks. `fzf` search finds approximate matches, so `ctrl-a` can select more than the entries you expect.

CPU and memory are live. Disk data comes from `docker system df`, which can take minutes on a large engine. A background job puts it in `~/.cache/wtree` when the data is older than 30 minutes, or after a delete. Press `ctrl-r` to see new data.

### Which stacks appear

- With `-g`, all stacks appear. A stack shows `worktree gone` if its directory no longer exists. The other rules in this list apply only without `-g`.
- A stack appears if its directory is in the repo or in one of its worktrees.
- A stack shows `worktree gone` if its directory is no longer in a Git repo and the repo name is in its stack name or path. Read the name before you delete it.
- A stack shows `volumes only` if it has no containers and its name starts with the repo name.
- Containers that Docker Compose did not start do not appear.

### Phone screens

Below 60 columns, each entry uses more lines, and long paths lose their start. `enter` or a tap opens a menu of actions.

To select several entries without a `tab` key, choose `select several` in the menu. Tap the entries, then press `enter` to open the menu for the selection.

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

4. Reload the configuration:

   ```sh
   rex config reload
   ```

The keys open `wtree` in a float, in the directory of the focused terminal. The actions are also in the command palette under "wtree". `git` and `fzf` must be on the `PATH` that your login shell sets.

To change the size of the float, use `require("wtree").setup{ bounds = { 0.1, 0.1, 0.8, 0.8 } }`. The values are x, y, width, and height, as fractions of the window.

To upgrade the plugin:

```sh
git -C ~/.config/rex/wtree pull
rex config reload
```

## Credits

The structure of the Rex plugin follows [rex-worktrunk](https://github.com/haxybaxy/rex-worktrunk) (MIT).

## License

MIT
