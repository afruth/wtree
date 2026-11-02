-- Opens the docker float of init.lua on the wrapper of real_float.sh, and prints the block.
local INIT, WRAP = os.getenv("WTREE_LUA_INIT"), os.getenv("WTREE_LUA_WRAP")
local actions = {}
local real_action = rex.action
rex.action = function(a) actions[a.name] = a end
local module = dofile(INIT)
module.setup{ script = WRAP }
rex.action = real_action

local view = rex.session.view{}
local shell = view.windows[1].focused_block_id
local result = actions.wtree_docker.run({ block_id = shell, client_id = "client:lab" })
for _, w in ipairs(rex.session.view{}.windows) do
  for _, layer in ipairs(w.layers) do
    if layer.layer_id == result.layer_id then print("block=" .. layer.blocks[1].block_id) end
  end
end
