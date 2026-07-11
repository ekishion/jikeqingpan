# 即刻轻盘

主要利用百度网盘青春版实现的临时中转网盘！

后端使用Go实现，前端采用简单的HTML页面实现！


# 部署

1. 编辑config.json文件，替换"baidu_cookie"的值，这是获取你百度网盘青春版文件列表的必要参数

2. 运行./cmd/bin/main 即可启动


# 配置
```json
{
  "port": 8080, #监听端口
  "bind_address": "127.0.0.1", #默认仅监听本机；远程访问请明确配置并增加认证/HTTPS
  "baidu_cookie": "", #百度网盘青春版Cookie
  "rate_limit_per_second": 10, #网页请求速度限制
  "baidu_app_id": "250528"
}
```

`config.json` 仅用于本地运行，自行创建




# UI截图
![](https://img11.360buyimg.com/ddimg/jfs/t1/443186/21/10502/30047/6a1fa645F590f15a3/001536a2fd6f69b0.jpg)


## 核心API

* 获取文件列表

```
import requests

url = "https://pan.baidu.com/youth/api/list?clienttype=0&app_id=250528&web=1&dp-logid=24365100307869040004&order=time&desc=1&num=20&page=1"

payload = {}
headers = {
  'Cookie': ''
}

response = requests.request("GET", url, headers=headers, data=payload)

print(response.text)
```


* 生成下载地址

```
import requests

url = "https://pan.baidu.com/youth/api/locatedownload?app_id=250528&dp-logid=24365100307869040007&rand=91a731462dbd805aaf2b6c543133bf1eabdc44d8&time=1739410749230&path=%2FTiny+RDM.exe&sign=5a932abca6dd247925afef5f7d65cef2"

payload = {}
headers = {
  'Cookie': ''
}

response = requests.request("GET", url, headers=headers, data=payload)

print(response.text)
```

* 参考 AList的BaiduYouth 驱动源码，获取了通过 uk/sk 以及 locatedownloadRand 与 locatedownloadSign 算法动态生成直链的关键逻辑。
