
import os

from flask import Flask, render_template


app = Flask(__name__, static_folder="static", template_folder="templates")


@app.route("/")
def index() -> str:
    return render_template("passenger.html")


@app.route("/passenger")
def passenger() -> str:
    return render_template("passenger.html")


@app.get("/favicon.ico")
def favicon() -> tuple[str, int]:
    return "", 204


@app.get("/.well-known/appspecific/com.chrome.devtools.json")
def chrome_devtools_config() -> tuple[str, int]:
    return "", 204


@app.route("/driver")
def driver() -> str:
    return render_template("driver.html")


def main() -> None:
    port = int(os.environ.get("PORT", "5000"))
    app.run(host="0.0.0.0", port=port)


if __name__ == "__main__":
    main()
