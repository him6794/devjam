import os

from google import genai
from google.genai import types
from google.cloud import texttospeech

GEMINI_API_KEY = os.environ.get("GEMINI_API_KEY")
if not GEMINI_API_KEY:
    from local_config import GEMINI_API_KEY  

_client = genai.Client(api_key=GEMINI_API_KEY)
_tts_client = texttospeech.TextToSpeechClient()

TEXT_MODEL = "gemini-flash-lite-latest"
TTS_VOICE_NAME = "cmn-TW-Wavenet-A"

PROMPT = """你是一個協助「隧道視野（視野狹窄）」患者的城市資訊助理。
使用者剛拍下一張照片，內容可能是路況看板、告示牌、路口、周遭環境或人群。
請用簡短、口語、適合直接語音播報的方式，摘要這張圖片裡最重要的資訊：

1. 如果畫面中有文字/告示，直接講出重點內容（例如列車時刻、封閉路段），不要唸出無關的排版符號。
2. 如果有潛在危險（施工、來車、階梯、人群擁擠），用方位詞明確指出方向（例如：「你的右前方有機車經過」）。
3. 全部輸出控制在 3 句話以內，語氣自然，像在跟朋友說話，不要加任何 Markdown 或標籤符號。
4. 數字（公車路線號碼、時間、樓層等）一律用阿拉伯數字（0-9）標示，不要寫成中文數字大寫（例如寫 262，不要寫兩百六十二）。
"""


def describe_image(image_path: str) -> str:
    with open(image_path, "rb") as f:
        image_bytes = f.read()

    mime_type = "image/png" if image_path.lower().endswith(".png") else "image/jpeg"

    response = _client.models.generate_content(
        model=TEXT_MODEL,
        contents=[
            types.Part.from_bytes(data=image_bytes, mime_type=mime_type),
            PROMPT,
        ],
    )
    return response.text.strip()


def synthesize_speech(text: str, output_path: str) -> str:
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


def run_pipeline(image_path: str, output_audio_path: str) -> dict:
    summary = describe_image(image_path)
    audio_path = synthesize_speech(summary, output_audio_path)
    return {"summary": summary, "audio_path": audio_path}



if __name__ == "__main__":
    import sys

    image_path = sys.argv[1] if len(sys.argv) > 1 else "test.jpg"
    result = run_pipeline(image_path, "output/demo.mp3")
    print("摘要：", result["summary"])
    print("語音檔：", result["audio_path"])
