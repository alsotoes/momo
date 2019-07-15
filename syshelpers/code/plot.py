import dateutil.parser, sys
from dateutil.parser import parse
from datetime import datetime, date
from dateutil import tz

f = open("/home/alvaro/Downloads/grafana_data_export.csv", "r")
for x in f:
    if x.find("Series") != -1:
        continue

    x_split = x.split(";")
    date_ = x_split[1].strip('""')
    dt = parse(date_)
  
    year = str(dt.date()).split("-")[0]
    month = str(dt.date()).split("-")[1]
    day = str(dt.date()).split("-")[2]

    hour = str(dt.time()).split(":")[0]
    minute = str(dt.time()).split(":")[1]
    seconds = str(dt.time()).split(":")[2]

    timestamp = datetime(int(year), int(month), int(day), int(hour), int(minute), int(seconds)).timestamp()

    date_time_str1 = "{}/{}/{} {}:{}:{}".format(int(year), int(month), int(day), int(hour), int(minute), int(seconds))
    date_time_obj1 = datetime.strptime(date_time_str1, '%Y/%m/%d %H:%M:%S')

    print(date_)
    #print(date_time_obj1)
    #print(timestamp, x_split[2].strip(), sep=",")
    #print("")

    # METHOD 1: Hardcode zones:
    from_zone = tz.gettz('CDT')
    to_zone = tz.gettz('UTC')

    # utc = datetime.utcnow()
    cdt = date_time_obj1

    # Tell the datetime object that it's in UTC time zone since 
    # datetime objects are 'naive' by default
    cdt = cdt.replace(tzinfo=from_zone)

    # Convert time zone
    utc = cdt.astimezone(to_zone)
    print(utc)
