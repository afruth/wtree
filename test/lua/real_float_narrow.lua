-- Gives the float of real_float.sh the width of a phone, and prints what the program reports to Rex.
rex.block.call("com.superlogical.terminal", "resize", { block_id = os.getenv("WTREE_LUA_BLOCK"), columns = 44, rows = 30 })
local status = rex.block.call("com.superlogical.terminal", "program_status", { block_id = os.getenv("WTREE_LUA_BLOCK") })
print(rex.json and rex.json.encode(status) or tostring(status and #(status.statuses or status) or status))
