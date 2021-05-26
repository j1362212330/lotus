LOTUS_PATH=/data/lotus/dev/.ldt0111 LOTUS_MINER_PATH=/data/lotus/dev/.mdt0111  ./lotus-miner  sn-storage   add \
 --mount-signal-uri="172.19.50.9:/data/cache/nfstest"  --mount-transf-uri="172.19.50.9:/data/cache/nfstest" \
--mount-dir="/data/nfs/" --max-size=1125899906842624 --sector-size=10240 --max-work=100  --mount-type="nfs" \
--mount-opt="-o rw,soft,timeo=30,retry=3"