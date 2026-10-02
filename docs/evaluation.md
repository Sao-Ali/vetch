# Evaluation plan

Vetch will compare cloud and edge inference under controlled, reproducible
conditions and evaluate whether request routing improves service behavior.

## Experimental controls

Cloud and edge tests should use the same model weights, prompts, generation
settings, and output-token limits wherever supported. Each result records:

- model precision and quantization;
- runtime and version;
- CPU allocation and runtime thread settings;
- actual execution device; and
- any backend-specific differences.

Required differences must be disclosed rather than described as identical
experiments.

## Measurements

- End-to-end request latency
- Backend execution time
- Time to first token
- Generated tokens per second
- Median and tail latency across repeated trials
- Peak memory
- Available compute-utilization counters
- Deployment and model-loading time
- Queue time and completed requests per second
- Failures and assignments by backend

Cold and warmed runs must be reported separately. Tests should cover multiple
concurrency levels. Cloud network time should be distinguished from model
execution time.

Output samples confirm that real inference ran, but answer quality and exact
text equality across runtimes are not the primary comparison. Power and energy
measurements are optional when reliable instrumentation is available. AWS cost
should be reported for the tested configuration rather than compared directly
with the purchase price of an edge board.

## Routing evaluation

The initial routing policy selects a healthy backend that supports the model and
has a free slot, using queue length to choose among eligible targets. Evaluation
compares this policy with fixed-backend routing under the same request load.

Report:

- completed requests per second;
- queueing time and tail latency;
- failures and retry attempts;
- assignments per backend; and
- behavior when a backend becomes unavailable.

Taking a backend offline must stop new assignments. In-flight failures should
use bounded retries or return a clear error, with duplicate attempts recorded.

## Stretch study

After the core platform and measurements work, the team may investigate whether
partitioning one inference across cloud and edge can outweigh network and
synchronization overhead. Feasibility or limitations are valid outcomes; the
project does not promise a speedup.
