"""
前端開發用的極簡 Flask 伺服器

這支檔案『只』負責渲染 templates/ 底下的 Jinja 頁面，
讓你在本機測試 passenger.html / driver.html 的畫面與流程。

它不包含任何真實的 /api/* 邏輯 —— 那些會由後端那組人實作。
前端目前串的是 static/js/mock-data.js 裡的假資料（USE_MOCK = true）。

跑法：
    pip install flask
    python server.py
    瀏覽器打開 http://localhost:5000/passenger
    另開一個分頁 http://localhost:5000/driver

等後端 API 寫好後，把這支檔案交給後端那組人合併，
或是把 static/、templates/ 整個資料夾丟進他們的 Flask 專案即可，
前端程式碼完全不用改，只要把 static/js/common.js 裡的
USE_MOCK 改成 false、API_BASE 填上正式後端網址。
"""

from flask import Flask, render_template

app = Flask(__name__)


@app.route("/")
def index():
    return render_template("passenger.html")


@app.route("/passenger")
def passenger():
    return render_template("passenger.html")


@app.route("/driver")
def driver():
    return render_template("driver.html")


if __name__ == "__main__":
    app.run(debug=True, port=5000)
