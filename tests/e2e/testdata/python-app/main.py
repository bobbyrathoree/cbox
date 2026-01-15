from fastapi import FastAPI
import os

app = FastAPI()
port = int(os.environ.get("PORT", 8000))


@app.get("/")
def root():
    return {"message": "Hello from cbox!", "runtime": "python"}


@app.get("/health")
def health():
    return {"status": "healthy"}


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=port)
