import dateutil.parser
from dateutil.parser import parse
from datetime import datetime, date

f = open("momo-server1_grafana.csv", "r")
for x in f:
  x_split = x.split(";")
  date_ = x_split[1].strip('""')
  dt = parse(date_)
  
  year = str(dt.date()).split("-")[0]
  month = str(dt.date()).split("-")[1]
  day = str(dt.date()).split("-")[2]

  hour = str(dt.time()).split(":")[0]
  minute = str(dt.time()).split(":")[1]

  timestamp = datetime(int(year), int(month), int(day), int(hour), int(minute)).timestamp()

  #print("{} {},{}".format(dt.date(), dt.time(), x_split[2]))
  #print("{},{}".format(timestamp,x_split[2].strip()))
  print(timestamp, x_split[2].strip(), sep=",")
