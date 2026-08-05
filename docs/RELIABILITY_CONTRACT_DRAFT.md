# HEC Minimal Recovery Contract — Draft

**Status:** unfrozen research draft.

This contract exists only to prevent a failed ChatGPT turn or MCP connection from killing or duplicating work that HEC already started. It is not a verification framework, workflow engine, audit system, or reliability platform.

## Rules

1. **Direct work stays direct.**
   Reads, file operations, Git operations, and short commands do not require task capsules, acceptance criteria, preflight phases, or verification hooks.

2. **Long work can be detached.**
   A caller may start a durable job and receive a handle as soon as HEC accepts it.

3. **Durable jobs outlive ChatGPT and the gateway.**
   The native process manager owns the process. HEC preserves the handle and output location needed to reconnect.

4. **Output is readable by offset.**
   A later ChatGPT turn can inspect job status and continue reading stdout and stderr without replaying the command.

5. **Job creation may use an idempotency key.**
   Repeating the same job-start request with the same key returns the existing handle instead of launching a duplicate.

6. **HEC does not automatically retry uncertain external side effects.**
   It reports the known process state and leaves the next action to ChatGPT and StealthEye.

7. **HEC reports facts, not universal proof.**
   Exit code, signal, output, file state, Git state, service state, and other observations are returned directly. Extra checks are run only when the caller or a specific skill requests them.

8. **Recovery state remains tiny.**
   HEC stores only the metadata needed to find a durable job, read its output, and return its current state. It does not maintain a general task ledger or model reasoning history.

9. **HEC remains standalone.**
   SEZU and Baby may construct HEC but are never runtime dependencies.

## Required initial test

1. Start a long-running durable command.
2. Disconnect or kill the MCP gateway.
3. Let the command continue.
4. Reconnect from a fresh ChatGPT turn.
5. Retrieve the same handle, status, and output.
6. Repeat the original start request with the same idempotency key and prove that no second process starts.

Anything beyond this test must justify its implementation cost with a real failure observed during ordinary use.
