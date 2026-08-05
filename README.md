# HEC

**Status:** research and architecture phase; not frozen.

HEC is a standalone, ChatGPT-native, unrestricted root engineering environment designed and operated by ChatGPT and StealthEye.

Its purpose is to give ChatGPT the broadest practical software-engineering, DevOps, systems, browser, data, document, media, cloud, networking, debugging, build, deployment, and infrastructure capability on an owner-controlled Linux host with the least possible friction and internal machinery.

## Core principle

> **Maximum capability. Minimum bullshit.**

HEC is intentionally large in tools and small in architecture.

## Founding contract

- The real host is the primary execution environment.
- ChatGPT receives unrestricted root execution as a first-class capability.
- There is no permanent forge container or required `u` environment.
- Containers, namespaces, VMs, and other isolation mechanisms remain available as ordinary tools when useful.
- HEC remains standalone. SEZU and Baby may bootstrap the build but are never runtime dependencies.
- HEC should include every useful free toolchain and professional capability that fits the machine, with on-demand installation available for everything else.
- Arbitrary commands, packages, source builds, services, daemons, containers, filesystems, networks, mounts, cloud clients, browsers, debuggers, compilers, and third-party tools remain directly usable.
- A raw unrestricted execution path always exists.
- Skills, manifests, schemas, and discovery exist only to help ChatGPT find and use capabilities; they never restrict them.
- Git, systemd, the filesystem, package managers, process state, and native tools remain authoritative. HEC does not duplicate them with a second control system.
- Direct work stays direct.
- Long-running commands may be detached and reconnected so a failed ChatGPT turn does not kill them.
- Public interfaces and results are designed specifically for ChatGPT: concise, structured where useful, binary-safe, and easy to continue from.

## Explicit exclusions

HEC does not add:

- policy engines;
- approval systems;
- command filters;
- safety layers;
- receipts;
- evidence systems;
- audit systems;
- reporting systems;
- mandatory previews;
- mandatory preflight;
- mandatory verification;
- automatic rollback;
- automatic reconciliation frameworks;
- task ledgers;
- reliability bureaucracy;
- governance theater;
- observability theater;
- abstractions that merely rename Linux, Git, systemd, containers, or files.

ChatGPT and StealthEye may inspect, test, verify, commit, snapshot, branch, or back up work whenever the actual task calls for it. Those are ordinary operations, not compulsory HEC subsystems.

## Design test

Every proposed component must answer at least one question:

1. Does it give ChatGPT a real new capability?
2. Does it make an existing capability materially easier or faster to use?
3. Is it strictly necessary for the ChatGPT connection or for work to survive a disconnected turn?

If the answer to all three is no, it does not belong in HEC.

## Current phase

HEC is being designed as a capability-complete root workstation and ChatGPT interface with the smallest implementation that can expose and operate that capability reliably enough for ordinary use.

Nothing beyond this founding contract is frozen yet.
