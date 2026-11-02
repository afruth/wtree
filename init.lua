-- wtree: worktree and Docker stack pickers for Rex.
--   require("wtree").setup{}
--   rex.bind("ctrl+space>w", "wtree")
--   rex.bind("ctrl+space>n", "wtree_new")
--   rex.bind("ctrl+space>d", "wtree_docker")
--   rex.bind("ctrl+space>g", "wtree_global")
--   rex.bind("ctrl+space>s", "wtree_docker_global")

local M = {}

M.options = {
  -- x, y, width, height of the float, as fractions of the window.
  bounds = { 0.1, 0.1, 0.8, 0.8 },
  -- The float on a narrow terminal, such as a phone: all of the window. A layer has no option for
  -- its frame, so the bounds are the only room that this plugin can give back to the list.
  narrow_bounds = { 0, 0, 1, 1 },
  -- Below this width of the terminal in columns, the float uses narrow_bounds.
  narrow_columns = 70,
  -- The script. Defaults to bin/wtree beside this file. The script finds wtree-tui beside itself or
  -- on PATH, and it uses fzf if there is none, so the float always starts the script.
  script = nil,
}

local SHELL    = "com.superlogical.terminal.shell"
local TERMINAL = "com.superlogical.terminal"

-- The lists of the whole machine are the same from any directory. A list of a repo is of that repo.
local ANY_DIRECTORY = {
  ["wtree all"] = true,
  ["wtree docker all"] = true,
}

local function fail(message)
  error("wtree: " .. message, 0)
end

-- Rex names a module's chunk by its absolute path, or by a path relative to the init.lua it opened.
local function module_dir()
  local src = (debug.getinfo(1, "S").source or ""):gsub("^@", "")
  local dir = src:match("^(/.*)/[^/]*$")
  if dir then return dir end
  local cfg = os.getenv("REX_CONFIG")
  local base = cfg and cfg:match("^(.*)/[^/]*$")
    or (os.getenv("XDG_CONFIG_HOME") or (os.getenv("HOME") .. "/.config")) .. "/rex"
  local rel = src:match("^(.*)/[^/]*$")
  return rel and (base .. "/" .. rel) or base
end

local function block_cwd(block_id)
  if not block_id then return nil end
  local p = rex.block.call(TERMINAL, "process", { block_id = block_id })
  if not p then return nil end
  local fg, child = p.foreground or {}, p.child or {}
  local cwd = fg.cwd or child.cwd
  if cwd and cwd ~= "" then return cwd end
  return nil
end

local function placed_blocks(view)
  local blocks = {}
  for _, window in ipairs(view.windows or {}) do
    for _, layer in ipairs(window.layers or {}) do
      for _, block in ipairs(layer.blocks or {}) do
        blocks[block.block_id] = { owner = block.owner, window_id = window.window_id }
      end
    end
  end
  return blocks
end

local function active_focus(view)
  for _, window in ipairs(view.windows or {}) do
    if window.window_id == view.active_window_id then return window.focused_block_id end
  end
  return nil
end

-- Focus is one value per session, so without a block from the client ctx.block_id
-- is the last focus of any device. No API gives a client's own focus, and nothing
-- tells a block that the client named from that last focus. The owner of a block is
-- the best sign of who uses it, so when another client owns ctx.block_id, take the
-- one focused block that the caller owns. This guess can be wrong, so the prompt of
-- the list names the repo. Two of them can't be told apart.
local function caller_block(ctx, view, blocks)
  local named = blocks[ctx.block_id]
  if ctx.block_id and (not named or not named.owner or named.owner == ctx.client_id) then
    return ctx.block_id
  end
  local mine = {}
  for _, window in ipairs(view.windows or {}) do
    local focused = blocks[window.focused_block_id]
    if focused and focused.owner and focused.owner == ctx.client_id then
      mine[#mine + 1] = window.focused_block_id
    end
  end
  if #mine == 1 then return mine[1] end
  return ctx.block_id or active_focus(view)
end

-- Rex loses Lua state on a config reload, so read the floats from the session.
-- A closed float hangs up the steps of a delete that runs in it, and Lua cannot see
-- that, so wtree never closes a float. The float of another client stays behind the
-- new one. Client IDs differ between servers, so with ctx.server the owner says
-- nothing, and a float from server Lua has no owner.
-- Only a float that shows what was asked for comes back: the same label, and the same
-- directory. Another float stays open behind the new one, for the same reason.
local function float_shows(block, label, cwd)
  if block.label ~= label then return false end
  if ANY_DIRECTORY[label] then return true end
  local ok, there = pcall(block_cwd, block.block_id)
  return ok and there == cwd
end

local function open_float_of(ctx, view, window_id, label, cwd)
  for _, window in ipairs(view.windows or {}) do
    for _, layer in ipairs(window.layers or {}) do
      local blocks = layer.blocks or {}
      local block = blocks[1] or {}
      if window.window_id == window_id and layer.kind == "floating" and #blocks == 1
        and block.flavor == SHELL and float_shows(block, label, cwd)
        and (ctx.server or not block.owner or block.owner == ctx.client_id) then
        return layer, block
      end
    end
  end
  return nil
end

-- The app can run an action with its own config for a session on another host, so
-- this Lua can run on a Mac while the float runs on Linux. Find a script under HOME
-- from $HOME on the host of the float. The float closes when the script exits, so
-- keep an error on screen until a key press.
local function script_command(args)
  local home, script = os.getenv("HOME") or "", M.options.script
  local prefix = ""
  if script:sub(1, 2) == "~/" then
    prefix, script = "$HOME/", script:sub(3)
  elseif home ~= "" and script:sub(1, #home + 1) == home .. "/" then
    prefix, script = "$HOME/", script:sub(#home + 2)
  end
  local command = {
    "/bin/sh", "-c",
    "script=" .. prefix .. '$1; shift; "$script" "$@" || { printf "\\nPress enter to close. "; read -r _; }',
    "wtree", script,
  }
  -- OSC 7501 reaches the Rex tab also when TERM_PROGRAM does not reach the float
  command[#command + 1] = "--rex"
  for _, arg in ipairs(args) do command[#command + 1] = arg end
  return command
end

-- The width that the caller asks for, or else the width of the terminal. nil if Rex does not say.
local function columns_of(ctx, block_id)
  if not block_id then return nil end
  local ok, size = pcall(rex.block.call, TERMINAL, "size", { block_id = block_id })
  if not ok or type(size) ~= "table" then return nil end
  for _, desired in ipairs(size.desired or {}) do
    if desired.client_id == ctx.client_id and (desired.columns or 0) > 0 then return desired.columns end
  end
  if (size.columns or 0) > 0 then return size.columns end
  return nil
end

local function bounds_for(ctx, block_id)
  local columns = columns_of(ctx, block_id)
  if columns and columns < M.options.narrow_columns then return M.options.narrow_bounds end
  return M.options.bounds
end

local function open_float(ctx, args, label)
  local view = rex.session.view{} or {}
  local blocks = placed_blocks(view)
  local block_id = caller_block(ctx, view, blocks)
  local cwd = block_cwd(block_id)
  if cwd == nil then fail("cannot find the directory of the focused terminal") end
  local window_id = (blocks[block_id] or {}).window_id or view.active_window_id
  local open, open_block = open_float_of(ctx, view, window_id, label, cwd)
  if open then
    rex.session.raise_layer{ layer_id = open.layer_id }
    rex.session.focus_block{ block_id = open_block.block_id }
    return { layer_id = open.layer_id }
  end
  local layer, err = rex.session.new_layer{
    window_id = window_id,
    bounds    = bounds_for(ctx, block_id),
    focus     = true,
    layout = {
      block = {
        flavor  = SHELL,
        label   = label,
        options = { command = script_command(args), cwd = cwd },
      },
    },
  }
  if err ~= nil then fail(tostring(err)) end
  return { layer_id = layer.layer_id }
end

function M.setup(opts)
  for key, value in pairs(opts or {}) do
    if M.options[key] == nil and key ~= "script" then
      rex.log("warn", "wtree: ignoring unknown option " .. tostring(key))
    else
      M.options[key] = value
    end
  end
  -- A size that the user chose holds on every screen, unless the user also chose a narrow one.
  if opts and opts.bounds and not opts.narrow_bounds then M.options.narrow_bounds = opts.bounds end
  M.options.script = M.options.script or (module_dir() .. "/bin/wtree")

  rex.action{
    name        = "wtree",
    title       = "wtree: Worktrees…",
    description = "Search the worktrees of the repo in the focused terminal. Open or delete a worktree.",
    category    = "wtree",
    keywords    = "git worktree branch",
    run = function(ctx) return open_float(ctx, {}, "wtree") end,
  }

  rex.action{
    name        = "wtree_new",
    title       = "wtree: New Worktree",
    description = "Create a worktree at the current HEAD and open it in a new tab.",
    category    = "wtree",
    keywords    = "git worktree create",
    run = function(ctx) return open_float(ctx, { "-n" }, "wtree new") end,
  }

  rex.action{
    name        = "wtree_docker",
    title       = "wtree: Docker Stacks…",
    description = "Search the Docker Compose stacks of the repo and its worktrees, with usage. Stop or delete a stack.",
    category    = "wtree",
    keywords    = "docker compose stack container",
    run = function(ctx) return open_float(ctx, { "-d" }, "wtree docker") end,
  }

  rex.action{
    name        = "wtree_global",
    title       = "wtree: All Worktrees…",
    description = "Search all the worktrees on this machine. Open or delete worktrees.",
    category    = "wtree",
    keywords    = "git worktree branch global machine",
    run = function(ctx) return open_float(ctx, { "-g" }, "wtree all") end,
  }

  rex.action{
    name        = "wtree_docker_global",
    title       = "wtree: All Docker Stacks…",
    description = "Search all the Docker Compose stacks on this machine, with usage. Stop or delete stacks.",
    category    = "wtree",
    keywords    = "docker compose stack container global machine",
    run = function(ctx) return open_float(ctx, { "-g", "-d" }, "wtree docker all") end,
  }

  return M
end

return M
