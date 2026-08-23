package fold

// The two scripts, and why they are scripts.
//
// Both do a read and a write that must not be separable. Append reads the open
// run's shape and then either extends it or replaces it, and expire finds a due
// run and then removes it — in each case a second replica arriving between the
// two halves would produce a duplicate record or a lost one. Redis runs a script
// to completion with nothing interleaved, which is the whole reason the logic is
// here rather than in three round trips.

// appendScript adds one record to its run.
//
// KEYS: 1 the run's hash, 2 its chunk hash, 3 the sorted set of open runs.
// ARGV: 1 shape, 2 text, 3 seq, 4 end (ms), 5 error, 6 dropped, 7 mergeable,
//
//	8 the record as JSON, 9 deadline (ms), 10 ttl (s), 11 max bytes.
//
// Returns the run it displaced — {hash fields, chunk fields} — or nil. A run is
// displaced only by a record whose shape differs, which is the boundary between
// two things being streamed one after the other.
const appendScript = `
local hash, chunks, pending, zset = KEYS[1], KEYS[2], KEYS[3], KEYS[4]
local shape, text, seq, endMs = ARGV[1], ARGV[2], tonumber(ARGV[3]), tonumber(ARGV[4])
local err, dropped, mergeable = ARGV[5], ARGV[6], ARGV[7]
local first, deadline, ttl, maxBytes = ARGV[8], tonumber(ARGV[9]), tonumber(ARGV[10]), tonumber(ARGV[11])
local minRun = tonumber(ARGV[12])

local closed = nil
local open = redis.call('HGET', hash, 'shape')

if open ~= false and open ~= shape then
  -- What is streaming has changed. Hand the finished run back whole, then fall
  -- through and start a new one from this record.
  closed = { redis.call('HGETALL', hash), redis.call('HGETALL', chunks),
             redis.call('HGETALL', pending) }
  redis.call('DEL', hash, chunks, pending)
  redis.call('ZREM', zset, hash)
  open = false
end

if open == false then
  redis.call('HSET', hash,
    'shape', shape, 'count', 1, 'lastSeq', seq, 'lastEnd', endMs,
    'bytes', 0, 'truncated', '', 'mergeable', mergeable,
    'err', err, 'dropped', dropped, 'first', first)
  redis.call('HSET', pending, seq, first)
  if mergeable ~= '' then
    redis.call('HSET', chunks, seq, text)
    redis.call('HSET', hash, 'bytes', #text)
  end
else
  local count = redis.call('HINCRBY', hash, 'count', 1)

  -- Held only while the run might still turn out to be too short to fold. Once it
  -- is long enough that it never will, the copies are dead weight and the folded
  -- record is what these rows become.
  if count < minRun then
    redis.call('HSET', pending, seq, first)
  elseif count == minRun then
    redis.call('DEL', pending)
  end

  -- Records arrive out of order across replicas, so the run's end is the greatest
  -- stamp seen rather than the latest one to turn up.
  if seq > tonumber(redis.call('HGET', hash, 'lastSeq')) then
    redis.call('HSET', hash, 'lastSeq', seq)
  end
  if endMs > tonumber(redis.call('HGET', hash, 'lastEnd')) then
    redis.call('HSET', hash, 'lastEnd', endMs)
  end

  -- First error in the run wins: it is the one that explains what went wrong,
  -- and the ones after it are usually consequences.
  if err ~= '' and redis.call('HGET', hash, 'err') == '' then
    redis.call('HSET', hash, 'err', err)
  end
  if dropped ~= '' then
    redis.call('HSET', hash, 'dropped', dropped)
  end

  -- A shape-matching record either carries a payload field or has none; both are
  -- ordinary, and one with none contributes the empty string it contributed to
  -- the stream. Whether the run merges at all was settled by its first record.
  if redis.call('HGET', hash, 'mergeable') ~= '' and #text > 0 then
    local used = tonumber(redis.call('HGET', hash, 'bytes'))
    if used + #text > maxBytes then
      redis.call('HSET', hash, 'truncated', '1')
    else
      redis.call('HSET', chunks, seq, text)
      redis.call('HSET', hash, 'bytes', used + #text)
    end
  end
end

-- Re-armed on every record, so the window measures silence rather than age: a
-- stream still producing frames keeps its run open however long it runs.
redis.call('ZADD', zset, deadline, hash)
redis.call('EXPIRE', hash, ttl)
redis.call('EXPIRE', chunks, ttl)
redis.call('EXPIRE', pending, ttl)

return closed
`

// expireScript pops every run whose window has passed.
//
// KEYS: 1 the sorted set of open runs. ARGV: 1 now (ms), 2 how many at most.
//
// Returns a list of {hash fields, chunk fields, pending fields}. Finding and
// removing in one script is what makes a folded record appear exactly once when
// several replicas sweep at the same moment.
const expireScript = `
local zset = KEYS[1]
local now, limit = tonumber(ARGV[1]), tonumber(ARGV[2])

local due = redis.call('ZRANGEBYSCORE', zset, '-inf', now, 'LIMIT', 0, limit)
local out = {}
for i = 1, #due do
  local hash = due[i]
  local fields = redis.call('HGETALL', hash)
  if #fields > 0 then
    out[#out + 1] = { fields,
                      redis.call('HGETALL', hash .. ':chunks'),
                      redis.call('HGETALL', hash .. ':pending') }
  end
  redis.call('DEL', hash, hash .. ':chunks', hash .. ':pending')
  redis.call('ZREM', zset, hash)
end
return out
`
