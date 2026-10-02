#!/bin/bash

echo -n "$(date): keepalive "
curl -fsS --max-time 20 \
  https://pittsburg-saildata.onrender.com/health
# echo
