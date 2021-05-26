LOTUS_PATH=/data/lotus/dev/.ldt0111 LOTUS_MINER_PATH=/data/lotus/dev/.mdt0111  ./lotus-miner  sn-storage   add \
 --mount-signal-uri="/data/lotus/dev/.sdt0111"  --mount-transf-uri="/data/lotus/dev/.sdt0111" \
--mount-dir="/data/nfs/" --max-size=1125899906842624 --sector-size=10240 --max-work=100