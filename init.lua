-- wtree: worktree and Docker stack pickers for Rex.
--   require("wtree").setup{}
--   rex.bind("ctrl+space>w", "wtree")
--   rex.bind("ctrl+space>n", "wtree_new")
--   rex.bind("ctrl+space>d", "wtree_docker")

local M = {}

M.options = {
  -- x, y, width, height of the float, as fractions of the window.
  bounds = { 0.1, 0.1, 0.8, 0.8 },
  -- The script. Defaults to bin/wtree beside this file.
  script = nil,
}

local SHELL    = "com.superlogical.terminal.shell"
local TERMINAL = "com.superlogical.terminal"

local state = { layer_id = nil }

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

local function close_float()
  if state.layer_id then
    rex.session.close_layer{ layer_id = state.layer_id }
    state.layer_id = nil
  end
end

-- The float closes when the script exits, so keep an error on screen until a key press.
local function open_float(ctx, args, label)
  local cwd = block_cwd(ctx.block_id)
  if cwd == nil then fail("cannot find the directory of the focused terminal") end
  close_float()
  local command = {
    "/bin/sh", "-c",
    '"$0" "$@" || { printf "\\nPress enter to close. "; read -r _; }',
    M.options.script,
  }
  for _, arg in ipairs(args) do command[#command + 1] = arg end
  local layer, err = rex.session.new_layer{
    bounds = M.options.bounds,
    focus  = true,
    layout = {
      block = {
        flavor  = SHELL,
        label   = label,
        options = { command = command, cwd = cwd },
      },
    },
  }
  if err ~= nil then fail(tostring(err)) end
  state.layer_id = layer.layer_id
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

  return M
end

return M
