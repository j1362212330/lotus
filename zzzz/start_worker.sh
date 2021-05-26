export NETIP="192.168.56.50"
nohup  ./lotus-worker --miner-repo=/data/lotus/dev/.mdt0111 --worker-repo="/data/lotus/dev/.cache/.lotusworker" \
--storage-repo="/data/lotus/dev/.cache/.lotusworker/push" \
run  --listen-addr="127.0.0.1:6000"  --id-file="/data/lotus/dev/.cache/.lotusworker/worker.id" \
--max-tasks=20    --parallel-addpiece=3    --parallel-precommit1=3    --parallel-precommit2=2    --parallel-commit=1  >> dev_worker.log 2>&1 &

echo "'tail -f worker.log' to see worker log"