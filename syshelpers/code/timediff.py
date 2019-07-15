import datetime

date_time_str1 = '2019/07/15 02:19:48.562124'
date_time_str2 = '2019/07/15 02:36:30.409169'
date_time_obj1 = datetime.datetime.strptime(date_time_str1, '%Y/%m/%d %H:%M:%S.%f')
date_time_obj2 = datetime.datetime.strptime(date_time_str2, '%Y/%m/%d %H:%M:%S.%f')

print date_time_obj2 - date_time_obj1

'''
a = datetime.datetime.now()
b = datetime.datetime.now()
delta = b - a
print delta
'''
