"""
核心流程：拍照 -> Gemini 多模態理解 -> Cloud Text-to-Speech 語音摘要
給隧道視野（視野狹窄）患者用的城市資訊助理 Demo。

圖片理解：Gemini API 金鑰驗證。
語音合成：Cloud Text-to-Speech，用 IAM(ADC) 驗證（跟 Firestore 同一套，
Cloud Run 上用內建服務身分，本機用 gcloud 登入）——比 Gemini 原生 TTS快約 3 倍。
"""
import os

from google import genai
from google.genai import types
from google.cloud import texttospeech

GEMINI_API_KEY = os.environ.get("GEMINI_API_KEY")
if not GEMINI_API_KEY:
    from local_config import GEMINI_API_KEY  # 本機開發用，Cloud Run 上用環境變數

_client = genai.Client(api_key=GEMINI_API_KEY)
_tts_client = texttospeech.TextToSpeechClient()

TEXT_MODEL = "gemini-flash-lite-latest"
TTS_VOICE_NAME = "cmn-TW-Wavenet-A"

PROMPT = """你是一個協助「隧道視野（視野狹窄）」患者的城市資訊助理。
使用者剛拍下一張照片，內容可能是路況看板、告示牌、路口、周遭環境或人群，
也可能同時有多台公車並排進站。
請用簡短、口語、適合直接語音播報的方式，摘要這張圖片裡最重要的資訊：

1. 如果畫面中有文字/告示，直接講出重點內容（例如列車時刻、封閉路段），不要唸出無關的排版符號。
2. 如果有潛在危險（施工、來車、階梯、人群擁擠），用方位詞明確指出方向（例如：「你的右前方有機車經過」）。
3. 如果畫面中同時看到多台公車：先列出你辨識到的每一台公車號碼與它的方位（左／中／右，或第幾台），
   如果提示裡有指定使用者要搭的路線號碼，明確講出「你要搭的 XX 號在你的哪個方位」；
   如果指定路線目前不在畫面中，直接說「目前看到的都不是 XX 號」，不要亂猜。
4. 全部輸出控制在 3 句話以內，語氣自然，像在跟朋友說話，不要加任何 Markdown 或標籤符號。
5. 數字（公車路線號碼、時間、樓層等）一律用阿拉伯數字（0-9）標示，不要寫成中文數字大寫（例如寫 262，不要寫兩百六十二）。
"""

# 上車後確認用的提示語：使用者已經在車上，重點不是「找車」而是「這台車對不對」，
# 輸出要更短、更明確（是/不是），語音聽起來像一個立即的安全確認，不是一般敘述。
CONFIRM_BOARDING_PROMPT = """你是一個協助「隧道視野（視野狹窄）」患者的城市資訊助理。
使用者剛剛上了公車，拍下車內看到的路線號碼顯示（例如車頭跑馬燈、車內看板、司機旁顯示幕）。
提示裡會告訴你使用者原本要搭的目標路線號碼。

請只回傳一句非常短、語氣清楚肯定的確認訊息：
- 如果畫面中的號碼跟目標路線相符：直接說「XX 號，上對車了」。
- 如果畫面中的號碼跟目標路線不符：直接說「這是 XX 號，不是你要搭的 YY 號，上錯車了」。
- 如果完全看不到號碼：直接說「看不到號碼，麻煩再拍一次車頭或車內顯示幕」。

不要加其他敘述、不要客套話、不要 Markdown，一句話講完，數字一律用阿拉伯數字。
"""


def describe_image(image_path: str, tdx_hint: str | None = None,
                    confirm_boarding: bool = False) -> str:
    """把照片丟給 Gemini 多模態模型，回傳語音友善的摘要文字。
    tdx_hint：來自 TDX 即時到站資料的候選範圍提示，用來縮小辨識範圍、提升準確度。
    confirm_boarding：True 時改用「上車後確認」的簡短是/否提示語。"""
    with open(image_path, "rb") as f:
        image_bytes = f.read()

    mime_type = "image/png" if image_path.lower().endswith(".png") else "image/jpeg"

    prompt = CONFIRM_BOARDING_PROMPT if confirm_boarding else PROMPT
    contents = [types.Part.from_bytes(data=image_bytes, mime_type=mime_type), prompt]
    if tdx_hint:
        contents.append(tdx_hint)

    response = _client.models.generate_content(model=TEXT_MODEL, contents=contents)
    return response.text.strip()


def synthesize_speech(text: str, output_path: str) -> str:
    """把文字轉成中文語音 mp3，存到 output_path。用 Cloud Text-to-Speech。"""
    response = _tts_client.synthesize_speech(
        input=texttospeech.SynthesisInput(text=text),
        voice=texttospeech.VoiceSelectionParams(
            language_code="cmn-TW",
            name=TTS_VOICE_NAME,
            ssml_gender=texttospeech.SsmlVoiceGender.FEMALE,
        ),
        audio_config=texttospeech.AudioConfig(audio_encoding=texttospeech.AudioEncoding.MP3),
    )
    os.makedirs(os.path.dirname(output_path), exist_ok=True)
    with open(output_path, "wb") as out:
        out.write(response.audio_content)
    return output_path


def run_pipeline(image_path: str, output_audio_path: str, tdx_hint: str | None = None,
                  confirm_boarding: bool = False) -> dict:
    summary = describe_image(image_path, tdx_hint=tdx_hint, confirm_boarding=confirm_boarding)
    audio_path = synthesize_speech(summary, output_audio_path)
    return {"summary": summary, "audio_path": audio_path}


# ✅ 本機單測：python pipeline.py test.jpg
if __name__ == "__main__":
    import sys

    image_path = sys.argv[1] if len(sys.argv) > 1 else "test.jpg"
    result = run_pipeline(image_path, "output/demo.mp3")
    print("摘要：", result["summary"])
    print("語音檔：", result["audio_path"])
