for i in {1..10}; do
  dig @78.46.210.232 p$i.msg.t.xtory.sbs TXT +short &
done
wait
