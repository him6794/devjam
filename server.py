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
