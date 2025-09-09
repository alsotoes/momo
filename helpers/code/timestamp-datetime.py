from datetime import datetime

# current date and time
now = datetime.now()

#timestamp = datetime.timestamp(now)
timestamp = datetime.strptime("2020-07-05 20:16:35", '%Y-%m-%d %H:%M:%S')
print("timestamp =", type(timestamp))
