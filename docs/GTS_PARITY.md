# GTS+ parity roadmap

This document tracks `toyota-diagnostics` against the pinned current Toyota GTS+ corpus used by `ghidra_rh850_analysis`.

Current comparison target: **GTS+ 2026.03.002.02** from the 2026-06-18 distribution.

The reverse-engineering source of truth remains `ghidra_rh850_analysis`. This repository consumes only clean derived metadata and implements runtime behavior.

## Current baseline

The universal bundle already preserves the current Toyota resolver and routing model well:

- NA/EU/JP vehicle databases and VIN decision tables;
- all **2,136 Toyota ECU category identities per region**;
- install sets and category+phase transport routes;
- P5/P6 standard support-family dispatch and P6 29-bit normal-fixed addressing;
- current-P5 session metadata;
- decoded ECU catalogs where a clean catalog has been generated;
- CAN Bus Check topology;
- GTS-derived DID/DTC names and P5 signal conversion metadata;
- DTC scan/clear, raw UDS, live Data List monitoring, VIN/vehicle selection, and mounted-ECU probing for supported families;
- Active Test browse/plan plus the subset with fully materialized fixed request geometry;
- current TSS3 Operation FFD and Image FFD acquisition/decoding;
- generic utility-family discovery/planning.

Current clean catalog coverage is:

| Region | Toyota categories | Decoded catalogs |
|---|---:|---:|
| NA | 2,136 | 135 |
| EU | 2,136 | 161 |
| JP | 2,136 | 143 |

The current GTS+ master contains **191 logical DLL roles / 6,194 category-role bindings**. The standalone runtime intentionally implements only roles whose semantics have been recovered strongly enough to execute without guessing.

For NA decoded catalogs, the current Active Test census is:

- **1,739** candidates total;
- **245** fixed-geometry `executable` rows;
- **1,407** `plan_only` rows;
- **87** `unresolved_static_plan` rows.

Those are static evidence grades. The runtime now live-materializes exact P5 direct-test payload width for **all 1,082 NA plan-only direct rows** from the exported selector-`0xCA` runtime-length probe. GTS+ records `received_length - 3` into `DataIdLengthList`; the standalone `UdsClient` already returns only the DID value bytes, so `len(value)` is the same N. The probe is independent of role-`0x08`'s initial-value mode, which is why the 13 mode-1/no-read controls are covered too. The 72 unresolved direct rows remain blocked. `executable` means static request geometry is complete; it does not assert live ECU support or bypass required Toyota session/authentication behavior.

## Main parity gaps

### 1. Transport matrix

The runtime now has Panda and J2534 raw-CAN backends behind the same diagnostic stack, but Toyota's transport matrix extends beyond the implemented classic-CAN ISO-TP paths.

NA current route census:

- 310 `iso15765-phase-family` routes;
- 51 `iso15765-29bit-normal-fixed` routes;
- 15 `canfd-iso15765-ps` routes;
- 10 `iso13400-ndis` (DoIP) routes;
- 93 routes whose `ChangeCommIf` controller semantics remain unrecovered.

Parity work:

- **done:** select transport independently of Toyota operation semantics while retaining Panda as the default backend;
- **done baseline:** J2534 v04.04 provider discovery plus raw 11/29-bit classic-CAN backend;
- add native J2534 ISO15765 where it improves adapter compatibility and older-protocol expansion;
- add CAN-FD ISO-TP and DoIP backends where Toyota routing selects them;
- recover the remaining `ChangeCommIf` controller families instead of coercing them into CAN.

### 2. Vehicle and mounted-ECU resolution across generations

Current P5/P6 vehicle resolution is substantially recovered. Remaining gaps include:

- P3/P4 type-41 live second-stage selection;
- the current master references legacy `SelectCarType.dll` / `SelectCarTypeVin.dll`, but those binaries are absent from the pinned GTS+/V18 corpus;
- live support probes currently execute only `p5-standard` and `p6-standard`;
- P3/P4 and P5 Subaru/Suzuki/Mazda/Hino modes remain represented but return `probe_unavailable` rather than being mislabeled unsupported.

### 3. Data List / live monitor parity

The CLI has a strong ordinary-P5 monitor path, but GTS+ supports more display and protocol families.

Missing work includes:

- broaden decoded catalogs beyond the current 135/161/143 regional subset;
- recover P4/P6 and partner-family signal conversion/presentation paths instead of applying the P5 decoder outside its proven scope;
- special-format signals and generation-specific Data List semantics;
- GTS-style recording/graphing/trigger behavior and stored-session integration.

### 4. Health Check

`toyota scan` is not yet GTS+ Health Check parity. GTS+ Health Check aggregates and stores substantially more than live DTCs, including DTC/FFD/Info Code/Operation History/Monitor Data and optional time-stamp data.

Needed:

- all-system mounted-ECU inventory using Toyota's live capability model;
- generic freeze-frame retrieval;
- Info Code / Operation History / VCH / RoB collection;
- report/store format independent of Toyota proprietary binaries;
- refresh/diff behavior over saved snapshots.

### 5. DTC and generic freeze-frame families

Current DTC reading is the ordinary UDS `ReadDTCInformation` path plus catalog decoding, with recovered clear behavior. GTS+ has many generation/category-specific DTC and FFD plugins.

Missing families include:

- generic per-DTC freeze-frame retrieval/decoding;
- historical/pending/time-series DTC variants;
- nonstandard DTC formats and older protocol families;
- P4/P5/P6 generic Image FFD surfaces outside the already-recovered TSS3 path.

### 6. Active Test execution

This is one of the largest practical gaps.

The biggest unlocks are:

- **done for ordinary P5 direct tests:** materialize `DataIdLengthList` N from Toyota's exact selector-`0xCA` `22 <DID>` support probe after explicit execution acknowledgement, including mode-1/no-initial-read controls, while preserving the static plan grade and minimum as validation;
- **done:** generate the default N-byte return-control mask from the recovered direct-test bit range using MSB0 numbering;
- materialize parameterized RoutineControl variable sources;
- finish multi-control value-write execution after the already-recovered initialization/group decomposition;
- implement P6 routine/direct execution semantics and support checks;
- finish role-specific stop/status/presentation paths where they materially affect execution.

### 7. Customize

GTS+ customization writes are effectively absent from the standalone runtime today.

Current GTS+ has direct customization command families such as `SetCustom.dll` and `SetCustomizeAllDefault_DT.dll`; the CLI currently exposes similarly named values only when they appear as ordinary Data List signals.

Needed:

- recover customize-item catalogs and presentation values;
- read current settings;
- write one selected setting with exact Toyota request geometry;
- restore-default/all-default flows;
- preserve explicit mutation acknowledgement and post-write verification.

### 8. Utilities / registration / learning / calibration

`utility list/plan` currently exports only the small recovered generic category-0 family and does not provide broad ECU-specific utility parity.

Needed:

- map the per-ECU function/detail hierarchy to concrete utility executors;
- recover simple-operation utility families, especially current P6;
- initialization, zero-point calibration, learning, registration, replacement workflows, check modes, and ECU-specific maintenance routines;
- exact lifecycle and SecurityAccess prerequisites per operation rather than a generic policy layer.

### 9. SecurityAccess and session families

The RE corpus contains substantially more Toyota authentication logic than the runtime currently uses.

Missing generic runtime support includes:

- current TSS/ADS AES SecurityAccess outside the dedicated Image-FFD path;
- Central-Gateway SecurityAccess;
- Subaru dual-path behavior;
- older Feistel-based diagnostic SecurityAccess;
- operation-specific lifecycle selection and keepalive behavior across generations.

These should be implemented only when an exported operation explicitly requires them.

### 10. Record of Behavior, Vehicle Control History, and other stored evidence

Current GTS+ contains direct RoB command roles and first-class stored-data surfaces. The standalone CLI does not yet expose generic:

- Record of Behavior read/delete;
- Vehicle Control History;
- Operation History;
- generic time-series FFD;
- saved TSE/GTSE-style diagnostic sessions or an open equivalent.

TSS3 Operation/Image FFD is an intentionally narrow exception already implemented.

### 11. Live CAN Bus Check

The bundle carries the OEM CAN Bus Check topology, but the CLI only renders that topology. It does not yet reproduce GTS+'s live network-health workflow or CAN-bus-specific DTC acquisition.

### 12. Calibration Update Wizard / ECU reprogramming

The runtime intentionally has **no** flash workflow today. GTS+/CUW parity would require a separate, carefully bounded implementation of:

- CUW package selection and integrity verification;
- current prepare/flash writer routes;
- SecurityAccess / ECUAuth / ServiceAuth / seed/nonce handling;
- erase/download/transfer/verify/reset sequencing;
- timing/retry/reconnect behavior;
- flash-recovery/resume state;
- TIS/RKS authorization and calibration-file acquisition where Toyota requires them.

Most of the static host-side machinery has already been recovered in `ghidra_rh850_analysis`, but this should come after diagnostic parity because a wrong diagnostic read is inconvenient while a wrong flash writer can strand an ECU.

### 13. Online Toyota/TIS services

GTS+ also relies on Toyota-hosted services for operations such as reprogramming authorization, calibration acquisition, and MACKey Registration. Those are not present in the standalone CLI and should stay a separate optional integration boundary rather than becoming a hard dependency for ordinary diagnostics.

## Recommended implementation order

1. **Finish transport breadth:** the Panda/J2534 raw-CAN abstraction is landed; next add native J2534 ISO15765 as needed, then CAN-FD/DoIP and older J2534 protocols.
2. **Health Check foundation:** generic mounted-ECU inventory, DTC + generic FFD + Info Code collection, durable open snapshots.
3. **Active Test completion:** ordinary P5 direct runtime length is landed; next parameterized routines, multi-control writes, and P6 execution.
4. **Customize.** High user value and relatively bounded compared with reflash.
5. **Utilities/registration/learning.** Add concrete operations family-by-family from exact recovered plugin semantics.
6. **RoB/VCH/generic FFD/stored-data parity.** Reuse the Health Check snapshot model.
7. **Broaden generation/protocol coverage:** P3/P4, partner P5 modes, CAN-FD/DoIP, remaining transport controllers.
8. **MACKey Registration** as an optional authenticated/TIS-backed workflow.
9. **CUW/reprogramming last**, with explicit recovery-oriented design and no coupling to ordinary diagnostic commands.

## J2534 / MVCI backend

**Implemented baseline (2026-09-12):** the CLI now has a first-class `--transport j2534` backend. It discovers Windows v04.04 providers or accepts an explicit shared library, opens a raw CAN channel with separate 11-bit and 29-bit pass filters, and presents the same CAN contract used by the existing opendbc ISO-TP/UDS layer. This gives the current DID/DTC/VIN/Active-Test/FFD operations a transport-independent path through J2534 without duplicating their protocol logic. The implementation was ABI-tested and load-tested against a locally built OpenMVCI dylib; no physical MVCI was attached for a live vehicle test.

Still missing on the J2534 side: native J2534 ISO15765 channels, CAN-FD/ISO15765-PS, ISO9141/ISO14230 for older Toyotas, DoIP, and live validation against the maintainer Mini-VCI clone.

J2534 is a natural backend, not an adapter-specific exception: Techstream/GTS+ itself uses the J2534 pass-thru model.

The desired runtime shape is:

```text
CLI / diagnostic operations
        |
        v
transport-neutral diagnostic session
        |
        +-- Panda backend
        +-- J2534 backend
        +-- future DoIP backend
```

The J2534 backend should support:

- device discovery/selection;
- `PassThruOpen` / `PassThruConnect` / filters / read/write / IOCTL configuration;
- CAN and ISO15765 first, then ISO9141/ISO14230 if needed for older Toyota generations;
- Toyota category routing from the same generated bundle used by Panda;
- functional and physical addressing;
- periodic tester-present where the Toyota lifecycle requires it;
- raw CAN receive mode for capture where the VCI supports it.

For an XHorse-style Mini-VCI clone, two implementation paths are useful:

1. **Vendor J2534 v04.04 DLL on Windows.** Load the registered `FunctionLibrary` and call the standard pass-thru ABI. Many clone packages expose `MVCI32.dll`, so process/DLL architecture must match.
2. **Cross-platform Mini-VCI implementation.** A compatible J2534-style shared library can be loaded on macOS/Linux and lets the rest of the CLI use the same backend API without requiring Techstream or a Windows VM.

Do not make the J2534 backend emulate Panda at the application layer. The core should depend on a small diagnostic-transport contract; each backend can use the best capabilities of its VCI while preserving identical Toyota operation semantics above it.
