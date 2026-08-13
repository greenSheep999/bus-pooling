# kirodrop 会话 token 刷新器 · captcha(ddddocr)→ login → 打印 token
#
# 为什么单独一个镜像：ddddocr 拖 onnxruntime + numpy + Pillow（~200MB）· 不该塞进主 app 镜像。
# 这个只在 cron 里一次性跑（docker run --rm）· 跑完即退。
#
# 构建（vps22 上）：docker build -t kdrop-refresh scripts/kirodrop-refresh
# 跑：docker run --rm -e KDROP_EMAIL=.. -e KDROP_PASSWORD=.. kdrop-refresh  → stdout 是新 token

FROM python:3.11-slim

# ddddocr 自带 ONNX 模型 · 离线识别 · 无需联网下模型（本地 python:3.11 实测 pip 装即可用）
RUN pip install --no-cache-dir ddddocr

COPY solve.py /solve.py

# 只有 token 进 stdout · 日志走 stderr（host 脚本用 $(docker run ...) 抓 token）
ENTRYPOINT ["python", "/solve.py"]
