"""
核心流程：拍照 -> Gemini 多模態理解 -> Gemini 原生 TTS 語音摘要
給隧道視野（視野狹窄）患者用的城市資訊助理 Demo。

全程只用一把 API 金鑰（Google AI Studio 拿到的 key），不需要 service account / IAM。
"""
import os
import wave

from google import genai
from google.genai import types

GEMINI_API_KEY = os.environ.get("GEMINI_API_KEY")
if not GEMINI_API_KEY:
    from local_config import GEMINI_API_KEY  # 本機開發用，Cloud Run 上用環境變數

_client = genai.Client(api_key=GEMINI_API_KEY)

TEXT_MODEL = "gemini-flash-lite-latest"
TTS_MODEL = "gemini-2.5-flash-preview-tts"
TTS_VOICE = "Kore"

PROMPT = """你是一個協助「隧道視野（視野狹窄）」患者的城市資訊助理。
使用者剛拍下一張照片，內容可能是路況看板、告示牌、路口、周遭環境或人群。
請用簡短、口語、適合直接語音播報的方式，摘要這張圖片裡最重要的資訊：

1. 如果畫面中有文字/告示，直接講出重點內容（例如列車時刻、封閉路段），不要唸出無關的排版符號。
2. 如果有潛在危險（施工、來車、階梯、人群擁擠），用方位詞明確指出方向（例如：「你的右前方有機車經過」）。
3. 全部輸出控制在 3 句話以內，語氣自然，像在跟朋友說話，不要加任何 Markdown 或標籤符號。
"""


def describe_image(image_path: str) -> str:
    """把照片丟給 Gemini 多模態模型，回傳語音友善的摘要文字。"""
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
    """把文字轉成中文語音 wav，存到 output_path。用 Gemini 原生 TTS 模型（同一把 API 金鑰）。"""
    response = _client.models.generate_content(
        model=TTS_MODEL,
        contents=text,
        config=types.GenerateContentConfig(
            response_modalities=["AUDIO"],
            speech_config=types.SpeechConfig(
                voice_config=types.VoiceConfig(
                    prebuilt_voice_config=types.PrebuiltVoiceConfig(voice_name=TTS_VOICE)
                )
            ),
        ),
    )
    pcm_data = response.candidates[0].content.parts[0].inline_data.data

    os.makedirs(os.path.dirname(output_path), exist_ok=True)
    with wave.open(output_path, "wb") as wf:
        wf.setnchannels(1)
        wf.setsampwidth(2)
        wf.setframerate(24000)
        wf.writeframes(pcm_data)
    return output_path


def run_pipeline(image_path: str, output_audio_path: str) -> dict:
    summary = describe_image(image_path)
    audio_path = synthesize_speech(summary, output_audio_path)
    return {"summary": summary, "audio_path": audio_path}


# ✅ 本機單測：python pipeline.py test.jpg
if __name__ == "__main__":
    import sys

    image_path = sys.argv[1] if len(sys.argv) > 1 else "test.jpg"
    result = run_pipeline(image_path, "output/demo.wav")
    print("摘要：", result["summary"])
    print("語音檔：", result["audio_path"])
