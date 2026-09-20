# Load testing

The experimental `bench` package provides two load-generation modes. Use
`Runner.Run` with `Scenario` for a fixed number of concurrent workers. Each worker
waits for its callback before starting another iteration, so its arrival rate
decreases when work slows down.

Use `Runner.RunOpenLoop` with `OpenLoopScenario` to offer work on a fixed schedule.
Arrival offsets are relative to the start of observation; equal offsets represent
a burst. A slow callback does not postpone later arrivals. This is the first
measurement building block for [RFC #11](https://github.com/agentscope-ai/agentscope-go/issues/11),
not an application scheduler or a complete agent-quality benchmark.

## Configuration

`ArrivalOffsets` must be nonempty, nonnegative, and nondecreasing. `MaxInFlight`,
`Timeout`, and `DrainTimeout` must be positive. The runner copies the configuration
and schedule before starting work; do not mutate them concurrently with that copy.

- `MaxInFlight` bounds admitted callback wrappers, including inspection of a
  returned error and publication of the result. A full limit rejects an arrival
  immediately. There is no generator queue. These rejections describe the load
  generator's limit, not a response from an inference server.
- `Timeout` starts at the scheduled arrival, so scheduler delay consumes it.
  Overdue arrivals are retained as timed out rather than rescheduled.
- `DrainTimeout` sets the observation deadline after the last scheduled arrival.
  An earlier caller deadline also bounds observation. Observation can end early
  once every arrival has been offered and every admitted wrapper has finished.

The callback receives a context with the applicable deadline. Return promptly on
cancellation, and keep returned errors' `Error`, `Is`, and `Unwrap` methods prompt.
Callbacks and error inspection run outside the result-bookkeeping lock. A callback
that ignores cancellation keeps its slot until its wrapper finishes; timeout does
not permit another callback to replace it while it is still running.

Go cannot forcibly stop arbitrary callbacks. On cancellation or drain expiry the
runner returns an immutable outcome snapshot, cancels outstanding contexts, and
may leave at most `MaxInFlight` wrappers from that invocation finishing in the
background. The caller must arrange their termination before starting repeated
runs. This limit does not establish that remote inference stopped when its client
context was canceled.

## Reading the report

`Results` contains one record per planned arrival, in schedule order, with
one-based iteration numbers. `Offered` counts records whose scheduled arrivals
were reached by observation, including local rejection and timeout. Arrivals
interrupted before their scheduled time remain `not_offered`. An already-canceled
caller starts no observation: all records are `not_offered` and `Offered` is zero.

| Status | Meaning |
|---|---|
| `succeeded` | Wrapper completion was recorded before its arrival deadline with no error or cancellation |
| `failed` | Wrapper completed with an ordinary callback error before the deadline |
| `rejected` | Arrival reached the generator while its in-flight limit was full |
| `timed_out` | Arrival expired before authorization, completion reached its arrival deadline, the callback returned a deadline error, or observation expired before a due arrival could start |
| `canceled` | Completion reported cancellation before its deadline, or caller interruption prevented a due arrival from starting |
| `unfinished` | Wrapper had been authorized but no completion was recorded before observation ended |
| `not_offered` | Observation did not reach this planned arrival |

An arrival deadline takes precedence over a returned cancellation or ordinary
error. Completion at the exact arrival deadline is timed out. At the observation
deadline, neither a new start authorization nor a completion is accepted; an
authorized wrapper still lacking a recorded result is unfinished. Caller
cancellation returns the partial report and its context error. Reaching only the
drain deadline returns the report with a nil error; inspect the outcomes.

`StartedAt` records synchronized start authorization, not physical function entry.
A goroutine paused after authorization may enter the callback later with an
already-canceled context. `FinishedAt` records accepted wrapper completion,
including error inspection and bookkeeping. `Latency` is the interval from
`ScheduledAt` to `FinishedAt`, including scheduler delay. A wrapper whose callback
returned but whose error inspection has stalled remains unfinished.

Records without accepted completion have zero `FinishedAt` and `Latency`.
Unstarted records also have zero `StartedAt`. Do not insert these zero values into
a completion-latency distribution or omit their outcomes from the report. Late
completion cannot change a returned report. `EndTime` is the observation endpoint,
not the time when every noncooperative callback or remote operation stopped.

## Reproducible measurements

The [runnable example](../pkg/agentscope/bench/open_loop_example_test.go) uses an
offline callback. Run it with:

```bash
go test ./pkg/agentscope/bench -run '^ExampleRunner_RunOpenLoop$' -count=1 -v
```

For agent measurements, create isolated mutable agent/session state for each
independent task. Reuse only clients and tools whose concurrency contracts permit
it. Keep model and server versions, quotas/hardware, prompts, task inputs, tool
configuration, arrival schedule, and time limits with the report.

A nil callback error is not evidence of task quality. This driver deliberately
does not produce a task-goodput scalar or merge token usage. Join its iteration
records with application scoring and accounting before comparing harness
policies. Report all planned and offered outcomes, dispatch delay, completion
latencies, and unfinished work; separate generator rejection from backend errors.
The RFC's task corpus, scoring integration, and joined quality/resource report
remain separate P0 contributions.
