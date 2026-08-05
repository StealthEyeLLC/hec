# HEC

**Status:** research and architecture phase; not frozen.

HEC is a standalone, ChatGPT-native, unrestricted root engineering environment designed and operated by ChatGPT and StealthEye.

Its purpose is to make ChatGPT exceptionally effective at software engineering, systems administration, DevOps, debugging, deployment, research, and artifact production on an owner-controlled Linux machine.

## Founding constraints

- The real host is the primary execution environment.
- Root execution is a first-class capability, not an escape from a constrained product.
- Isolation may be used when technically useful, but no permanent forge container is required.
- HEC remains standalone. SEZU and Baby may bootstrap the build but are never runtime dependencies.
- Every interface, schema, result, workflow, observation, and recovery path is designed specifically for ChatGPT.
- Safety theater, governance theater, mandatory previews, hidden policy, and unnecessary friction are excluded.
- The system must minimize errors before action and recover cleanly when failures still occur.
- No ChatGPT turn may be the sole owner of durable work.
- Retries must not duplicate side effects.
- Current machine state outranks conversational assumptions.
- Success means verified real-world completion, not merely a successful command exit.

## Current phase

HEC is undergoing open-ended research, failure modeling, architecture synthesis, and model-interface testing before its v1 contract is frozen.

Nothing beyond this founding brief is canonical yet.
