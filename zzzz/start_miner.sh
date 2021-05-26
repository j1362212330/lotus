nohup ./lotus-miner --repo=/data/lotus/dev/.ldt0111 --miner-repo=/data/lotus/dev/.mdt0111 run --nosync >> dev_miner.log 2>&1 & 
echo "'tail -f dev_miner.log' to see the miner log"