# HEC

**Status:** research and architecture phase; not frozen.

HEC is a standalone, ChatGPT-native, unrestricted root engineering environment designed and operated by ChatGPT and StealthEye.

Its purpose is to make ChatGPT exceptionally effective at software engineering, systems administration, DevOps, debugging, deployment, research, and artifact production on an owner-controlled Linux machine.

## Founding constraints

- The real host is the primary execution environment.
- Root execution is a first-class capability, not an escape from a constrained product.
- Isolation may be used when technically useful, but no permanent forge container is required.
- HEC remains standalone. SEZU and Baby may bootstrap the build but are never runtime dependencies.
- Every public interface and result is designed specifically for ChatGPT.
- Minimum friction is a product requirement.
- Mandatory preflight, verification, approval, policy, audit, reporting, and rollback machinery are excluded.
- Direct operations remain direct. HEC adds structure only where it clearly improves ChatGPT's ability to execute or reconnect.
- Long-running work may be detached, durable, and reconnectable.
- A failed ChatGPT turn must not kill durable work that already started.
- HEC reports actual command, process, file, Git, service, browser, and job state without claiming universal correctness.
- HEC does not automatically retry uncertain external side effects.
- Current machine state outranks conversational assumptions.

## Current phase

HEC is being reduced to the smallest useful root-first vertical slice before broader capabilities are designed.

Nothing beyond this founding brief is canonical yet.
