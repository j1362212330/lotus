  
nohup ./lotus --repo=/data/lotus/dev/.ldt0111 daemon --api 3000 --bootstrap=false >> dev_daemon.log  2>&1  &
echo "'tail -f dev_daemon.log' to see the daemon log"