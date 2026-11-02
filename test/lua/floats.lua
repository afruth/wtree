-- The floats of init.lua: first against a fake rex table, then on the private server of run.sh.
-- In a table: in the Lua of Rex, a pcall that catches an error can cut a local of the main chunk off
-- from the closures that share it, so the count at the end would be wrong.
local count = { failed = 0, passed = 0 }

local function check(name, ok, detail)
  if ok then
    count.passed = count.passed + 1
    print("ok     " .. name)
  else
    count.failed = count.failed + 1
    print("FAIL   " .. name .. (detail and ("  " .. tostring(detail)) or ""))
  end
end

local INIT, STUB = os.getenv("WTREE_LUA_INIT"), os.getenv("WTREE_LUA_STUB")
local SHELL = "com.superlogical.terminal.shell"
local real_rex = rex

-- args: the session view, and the cwd of each block. Returns the actions and the calls that change Rex.
local function load_with(view, cwds, sizes, options)
  local calls = { closed = {}, new = {}, focused = {}, raised = {} }
  local actions = {}
  rex = {
    log = function() end,
    action = function(a) actions[a.name] = a end,
    block = { call = function(_, method, args)
      if method == "size" then
        local size = (sizes or {})[args.block_id]
        if size == "raises" then error("no such method") end
        return size
      end
      return { foreground = { cwd = cwds[args.block_id] } }
    end },
    session = {
      view = function() return view end,
      close_layer = function(a) calls.closed[#calls.closed + 1] = a.layer_id end,
      new_layer = function(a)
        calls.new[#calls.new + 1] = a
        return { layer_id = "new" }
      end,
      raise_layer = function(a) calls.raised[#calls.raised + 1] = a.layer_id end,
      focus_block = function(a) calls.focused[#calls.focused + 1] = a.block_id end,
    },
  }
  local module = dofile(INIT)
  local opts = { script = STUB }
  for key, value in pairs(options or {}) do opts[key] = value end
  module.setup(opts)
  return actions, calls
end

local function window_with(float)
  local layers = { { kind = "tiled", layer_id = "tiled", blocks = { { block_id = "S1", owner = "mac" } } } }
  if float then layers[2] = float end
  return { active_window_id = "W1", windows = { { window_id = "W1", focused_block_id = "S1", layers = layers } } }
end

local function float(label, owner, flavor)
  return { kind = "floating", layer_id = "F", blocks = { { block_id = "FB", label = label, owner = owner, flavor = flavor or SHELL } } }
end

local cwds = { S1 = "/repoA", S2 = "/repoB", FB = "/repoA" }
local ctx = { block_id = "S1", client_id = "mac" }

do
  local actions, calls = load_with(window_with(float("wtree", "mac")), cwds)
  local result = actions.wtree.run(ctx)
  check("the own float is focused and not closed",
    #calls.closed == 0 and #calls.new == 0 and calls.focused[1] == "FB" and calls.raised[1] == "F" and result.layer_id == "F")
end

do
  local actions, calls = load_with(window_with(float("wtree", "mac")), cwds)
  actions.wtree_new.run(ctx)
  check("wtree_new starts a new float, and does not raise the list",
    #calls.closed == 0 and #calls.raised == 0 and #calls.focused == 0 and #calls.new == 1 and calls.new[1].layout.block.label == "wtree new")
end

do
  local actions, calls = load_with(window_with(float("wtree docker all", "mac")), cwds)
  actions.wtree.run(ctx)
  check("a list of another scope stays closed, and the list asked for opens",
    #calls.closed == 0 and #calls.raised == 0 and #calls.new == 1 and calls.new[1].layout.block.label == "wtree")
end

do
  local other = { S1 = "/repoB", FB = "/repoA" }
  local actions, calls = load_with(window_with(float("wtree", "mac")), other)
  actions.wtree.run(ctx)
  check("from another repo the list of the first repo does not come back: its delete keys are for that repo",
    #calls.closed == 0 and #calls.raised == 0 and #calls.new == 1 and calls.new[1].layout.block.options.cwd == "/repoB")
end

do
  local other = { S1 = "/repoB", FB = "/repoA" }
  local actions, calls = load_with(window_with(float("wtree all", "mac")), other)
  actions.wtree_global.run(ctx)
  check("the list of the machine is the same from any directory", #calls.new == 0 and calls.raised[1] == "F")
end

do
  local actions, calls = load_with(window_with(float("wtree", nil)), cwds)
  actions.wtree.run({ block_id = "S1", client_id = "phone" })
  check("a float with no owner, as server Lua makes it, is not closed", #calls.closed == 0 and calls.focused[1] == "FB")
end

do
  local actions, calls = load_with(window_with(float("wtree", "phone")), cwds)
  actions.wtree.run(ctx)
  check("the float of another client stays, and a new one opens", #calls.closed == 0 and #calls.new == 1 and #calls.focused == 0)
end

do
  local actions, calls = load_with(window_with(float("wtree", "phone")), cwds)
  actions.wtree.run({ block_id = "S1", client_id = "mac", server = "lab" })
  check("with ctx.server the owner says nothing, and the float is focused", #calls.closed == 0 and calls.focused[1] == "FB")
end

do
  local actions, calls = load_with(window_with(float("notes", "mac")), cwds)
  actions.wtree.run(ctx)
  check("a float with another label is left alone", #calls.closed == 0 and #calls.focused == 0 and #calls.new == 1)
end

do
  local view = window_with(nil)
  view.windows[2] = { window_id = "W2", focused_block_id = "S2", layers = { { kind = "tiled", layer_id = "t2", blocks = { { block_id = "S2", owner = "phone" } } } } }
  local actions, calls = load_with(view, cwds)
  actions.wtree.run({ block_id = "S1", client_id = "phone" })
  local opened = calls.new[1]
  -- no API can tell this from the phone working in W1, so this states the guess, and README warns of it
  check("the block that the caller owns wins over the last focus", opened.window_id == "W2" and opened.layout.block.options.cwd == "/repoB")
end

do
  local actions, calls = load_with(window_with(nil), cwds)
  actions.wtree.run(ctx)
  check("the block that the caller owns and names is used", calls.new[1].window_id == "W1" and calls.new[1].layout.block.options.cwd == "/repoA")
end

local function bounds_with(sizes, options, actor)
  local actions, calls = load_with(window_with(nil), cwds, { S1 = sizes }, options)
  actions.wtree.run(actor or ctx)
  return table.concat(calls.new[1].bounds, " ")
end

check("a wide terminal gets the normal bounds", bounds_with({ columns = 120 }) == "0.1 0.1 0.8 0.8")
check("a narrow terminal gets all of the window", bounds_with({ columns = 50 }) == "0 0 1 1")
check("the size that the caller asks for counts over the size of the owner",
  bounds_with({ columns = 200, desired = { { client_id = "phone", columns = 44 } } }, nil, { block_id = "S1", client_id = "phone" }) == "0 0 1 1")
check("no size from Rex gives the normal bounds", bounds_with(nil) == "0.1 0.1 0.8 0.8")
check("a failed size call gives the normal bounds", bounds_with("raises") == "0.1 0.1 0.8 0.8")
check("a zero size gives the normal bounds", bounds_with({ columns = 0 }) == "0.1 0.1 0.8 0.8")
check("bounds that the user chose hold on a narrow terminal", bounds_with({ columns = 50 }, { bounds = { 0.2, 0.2, 0.6, 0.6 } }) == "0.2 0.2 0.6 0.6")
check("narrow bounds that the user chose are used", bounds_with({ columns = 50 }, { narrow_bounds = { 0.05, 0.05, 0.9, 0.9 } }) == "0.05 0.05 0.9 0.9")

do
  local actions, calls = load_with(window_with(nil), cwds)
  local expected = {
    wtree = "--rex", wtree_new = "--rex -n", wtree_docker = "--rex -d", wtree_global = "--rex -g", wtree_docker_global = "--rex -g -d",
  }
  for name, want in pairs(expected) do
    calls.new = {}
    actions[name].run(ctx)
    local command = calls.new[1].layout.block.options.command
    check("the float of " .. name .. " starts the script with " .. want,
      command[1] == "/bin/sh" and STUB:sub(-#command[5]) == command[5] and table.concat(command, " ", 6) == want,
      table.concat(command, " "))
  end
end

-- ---- the real API, on the private server of run.sh

rex = real_rex
do
  local actions = {}
  local real_action = rex.action
  rex.action = function(a) actions[a.name] = a end
  local module = dofile(INIT)
  module.setup{ script = STUB }
  rex.action = real_action

  local view = rex.session.view{}
  local window = view.windows[1]
  local shell = window.focused_block_id
  local actor = { block_id = shell, client_id = "client:lab" }
  local first = actions.wtree.run(actor)
  -- the block belongs to the client of this script, so the second press comes from that client
  for _, w in ipairs(rex.session.view{}.windows) do
    for _, layer in ipairs(w.layers) do
      if layer.layer_id == first.layer_id then actor.client_id = layer.blocks[1].owner end
    end
  end
  local second = actions.wtree.run(actor)
  local floats, focused = 0, nil
  view = rex.session.view{}
  for _, w in ipairs(view.windows) do
    if w.window_id == window.window_id then
      focused = w.focused_block_id
      for _, layer in ipairs(w.layers) do
        if layer.kind == "floating" then
          floats = floats + 1
          check("the float keeps its first label", layer.blocks[1].label == "wtree", layer.blocks[1].label)
          check("the float has the focus on the real server", focused == layer.blocks[1].block_id, focused)
        end
      end
    end
  end
  check("a second key press keeps one float on the real server", floats == 1 and first.layer_id == second.layer_id, floats)
end

print(string.format("lua tests: %d passed, %d failed", count.passed, count.failed))
