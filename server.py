import os
from datetime import datetime, timedelta, timezone
from typing import List, Optional

from fastapi import FastAPI, HTTPException, Depends, status
from fastapi.middleware.cors import CORSMiddleware
from fastapi.security import OAuth2PasswordBearer, OAuth2PasswordRequestForm
from sqlalchemy import create_engine, Column, Integer, String, Text, DateTime, ForeignKey, desc
from sqlalchemy.ext.declarative import declarative_base
from sqlalchemy.orm import sessionmaker, Session
from jose import JWTError, jwt
from passlib.context import CryptContext
from pydantic import BaseModel

# --- Configuration ---
SECRET_KEY = os.getenv("SECRET_KEY", "super-secret-key-change-me")
ALGORITHM = "HS256"
ACCESS_TOKEN_EXPIRE_MINUTES = 30
DATABASE_URL = os.getenv("DATABASE_URL", "sqlite:///./hackreddit.db")
PORT = int(os.getenv("PORT", 8080))  # ← matches your Go client

engine = create_engine(DATABASE_URL, connect_args={"check_same_thread": False})
SessionLocal = sessionmaker(autocommit=False, autoflush=False, bind=engine)
Base = declarative_base()

app = FastAPI(title="HackReddit API")

# CORS – allow all (adjust if needed)
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=True,
    allow_methods=["*"],
    allow_headers=["*"],
)

# --- Security ---
pwd_context = CryptContext(schemes=["bcrypt"], deprecated="auto")
oauth2_scheme = OAuth2PasswordBearer(tokenUrl="token")

# --- SQLAlchemy Models ---
class User(Base):
    __tablename__ = "users"
    id = Column(Integer, primary_key=True, index=True)
    username = Column(String, unique=True, index=True, nullable=False)
    hashed_password = Column(String, nullable=False)
    created_at = Column(DateTime, default=lambda: datetime.now(timezone.utc))

class Post(Base):
    __tablename__ = "posts"
    id = Column(Integer, primary_key=True, index=True)
    title = Column(String, nullable=False)
    content = Column(Text, nullable=False)
    author_id = Column(Integer, ForeignKey("users.id"), nullable=False)
    created_at = Column(DateTime, default=lambda: datetime.now(timezone.utc))
    upvotes = Column(Integer, default=0)
    downvotes = Column(Integer, default=0)

class Comment(Base):
    __tablename__ = "comments"
    id = Column(Integer, primary_key=True, index=True)
    content = Column(Text, nullable=False)
    post_id = Column(Integer, ForeignKey("posts.id"), nullable=False)
    author_id = Column(Integer, ForeignKey("users.id"), nullable=False)
    created_at = Column(DateTime, default=lambda: datetime.now(timezone.utc))
    upvotes = Column(Integer, default=0)
    downvotes = Column(Integer, default=0)

class Vote(Base):
    __tablename__ = "votes"
    id = Column(Integer, primary_key=True, index=True)
    user_id = Column(Integer, ForeignKey("users.id"), nullable=False)
    post_id = Column(Integer, ForeignKey("posts.id"), nullable=True)
    comment_id = Column(Integer, ForeignKey("comments.id"), nullable=True)
    value = Column(Integer, nullable=False)

Base.metadata.create_all(bind=engine)

# --- Pydantic Schemas ---
class UserCreate(BaseModel):
    username: str
    password: str

class UserOut(BaseModel):
    id: int
    username: str
    created_at: datetime
    class Config: from_attributes = True

class PostCreate(BaseModel):
    title: str
    content: str

class PostOut(BaseModel):
    id: int
    title: str
    content: str
    author: str
    created_at: datetime
    upvotes: int
    downvotes: int
    comment_count: int = 0
    class Config: from_attributes = True

class CommentCreate(BaseModel):
    content: str

class CommentOut(BaseModel):
    id: int
    content: str
    author: str
    created_at: datetime
    upvotes: int
    downvotes: int
    class Config: from_attributes = True

# --- Helpers ---
def get_db():
    db = SessionLocal()
    try:
        yield db
    finally:
        db.close()

def verify_password(plain, hashed):
    return pwd_context.verify(plain, hashed)

def get_password_hash(password):
    return pwd_context.hash(password)

def authenticate_user(db, username, password):
    user = db.query(User).filter(User.username == username).first()
    if not user or not verify_password(password, user.hashed_password):
        return None
    return user

def create_access_token(data: dict, expires_delta: Optional[timedelta] = None):
    to_encode = data.copy()
    expire = datetime.now(timezone.utc) + (expires_delta or timedelta(minutes=ACCESS_TOKEN_EXPIRE_MINUTES))
    to_encode.update({"exp": expire})
    return jwt.encode(to_encode, SECRET_KEY, algorithm=ALGORITHM)

async def get_current_user(token: str = Depends(oauth2_scheme), db: Session = Depends(get_db)):
    credentials_exception = HTTPException(
        status_code=status.HTTP_401_UNAUTHORIZED,
        detail="Could not validate credentials",
        headers={"WWW-Authenticate": "Bearer"},
    )
    try:
        payload = jwt.decode(token, SECRET_KEY, algorithms=[ALGORITHM])
        username = payload.get("sub")
        if username is None:
            raise credentials_exception
    except JWTError:
        raise credentials_exception
    user = db.query(User).filter(User.username == username).first()
    if user is None:
        raise credentials_exception
    return user

# --- Endpoints ---
@app.get("/health")
def health():
    return {"status": "ok"}

@app.post("/register", response_model=UserOut)
def register(user: UserCreate, db: Session = Depends(get_db)):
    if db.query(User).filter(User.username == user.username).first():
        raise HTTPException(400, "Username already taken")
    hashed = get_password_hash(user.password)
    db_user = User(username=user.username, hashed_password=hashed)
    db.add(db_user)
    db.commit()
    db.refresh(db_user)
    return db_user

@app.post("/token")
def login(form_data: OAuth2PasswordRequestForm = Depends(), db: Session = Depends(get_db)):
    user = authenticate_user(db, form_data.username, form_data.password)
    if not user:
        raise HTTPException(401, "Incorrect username or password")
    access_token = create_access_token(data={"sub": user.username})
    return {"access_token": access_token, "token_type": "bearer"}

@app.get("/me", response_model=UserOut)
def me(current_user: User = Depends(get_current_user)):
    return current_user

@app.get("/posts", response_model=List[PostOut])
def get_posts(skip: int = 0, limit: int = 20, sort: str = "newest", db: Session = Depends(get_db)):
    query = db.query(Post)
    if sort == "top":
        query = query.order_by(desc(Post.upvotes - Post.downvotes))
    else:
        query = query.order_by(desc(Post.created_at))
    posts = query.offset(skip).limit(limit).all()
    result = []
    for p in posts:
        author = db.query(User).filter(User.id == p.author_id).first()
        comment_count = db.query(Comment).filter(Comment.post_id == p.id).count()
        result.append(PostOut(
            id=p.id,
            title=p.title,
            content=p.content,
            author=author.username if author else "deleted",
            created_at=p.created_at,
            upvotes=p.upvotes,
            downvotes=p.downvotes,
            comment_count=comment_count
        ))
    return result

@app.post("/posts", response_model=PostOut)
def create_post(post: PostCreate, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    db_post = Post(title=post.title, content=post.content, author_id=current_user.id)
    db.add(db_post)
    db.commit()
    db.refresh(db_post)
    return PostOut(
        id=db_post.id,
        title=db_post.title,
        content=db_post.content,
        author=current_user.username,
        created_at=db_post.created_at,
        upvotes=db_post.upvotes,
        downvotes=db_post.downvotes,
        comment_count=0
    )

@app.get("/posts/{post_id}", response_model=PostOut)
def get_post(post_id: int, db: Session = Depends(get_db)):
    p = db.query(Post).filter(Post.id == post_id).first()
    if not p:
        raise HTTPException(404, "Post not found")
    author = db.query(User).filter(User.id == p.author_id).first()
    comment_count = db.query(Comment).filter(Comment.post_id == p.id).count()
    return PostOut(
        id=p.id,
        title=p.title,
        content=p.content,
        author=author.username if author else "deleted",
        created_at=p.created_at,
        upvotes=p.upvotes,
        downvotes=p.downvotes,
        comment_count=comment_count
    )

@app.get("/posts/{post_id}/comments", response_model=List[CommentOut])
def get_comments(post_id: int, db: Session = Depends(get_db)):
    if not db.query(Post).filter(Post.id == post_id).first():
        raise HTTPException(404, "Post not found")
    comments = db.query(Comment).filter(Comment.post_id == post_id).order_by(desc(Comment.created_at)).all()
    result = []
    for c in comments:
        author = db.query(User).filter(User.id == c.author_id).first()
        result.append(CommentOut(
            id=c.id,
            content=c.content,
            author=author.username if author else "deleted",
            created_at=c.created_at,
            upvotes=c.upvotes,
            downvotes=c.downvotes
        ))
    return result

@app.post("/posts/{post_id}/comments", response_model=CommentOut)
def create_comment(post_id: int, comment: CommentCreate, current_user: User = Depends(get_current_user), db: Session = Depends(get_db)):
    if not db.query(Post).filter(Post.id == post_id).first():
        raise HTTPException(404, "Post not found")
    db_comment = Comment(content=comment.content, post_id=post_id, author_id=current_user.id)
    db.add(db_comment)
    db.commit()
    db.refresh(db_comment)
    return CommentOut(
        id=db_comment.id,
        content=db_comment.content,
        author=current_user.username,
        created_at=db_comment.created_at,
        upvotes=db_comment.upvotes,
        downvotes=db_comment.downvotes
    )

# --- Vote endpoint (query params – works with Go client) ---
@app.post("/vote")
def vote(
    post_id: Optional[int] = None,
    comment_id: Optional[int] = None,
    value: int = 1,
    current_user: User = Depends(get_current_user),
    db: Session = Depends(get_db)
):
    if (post_id is None and comment_id is None) or (post_id is not None and comment_id is not None):
        raise HTTPException(400, "Vote must be on either post or comment")
    if value not in (1, -1):
        raise HTTPException(400, "Value must be 1 or -1")

    if post_id:
        target = db.query(Post).filter(Post.id == post_id).first()
        if not target:
            raise HTTPException(404, "Post not found")
    else:
        target = db.query(Comment).filter(Comment.id == comment_id).first()
        if not target:
            raise HTTPException(404, "Comment not found")

    existing = db.query(Vote).filter(
        Vote.user_id == current_user.id,
        Vote.post_id == post_id if post_id else None,
        Vote.comment_id == comment_id if comment_id else None
    ).first()

    if existing:
        if existing.value == value:
            db.delete(existing)
            if post_id:
                if value == 1: target.upvotes -= 1
                else: target.downvotes -= 1
            else:
                if value == 1: target.upvotes -= 1
                else: target.downvotes -= 1
            db.commit()
            return {"message": "Vote removed"}
        else:
            if post_id:
                if existing.value == 1:
                    target.upvotes -= 1; target.downvotes += 1
                else:
                    target.downvotes -= 1; target.upvotes += 1
            else:
                if existing.value == 1:
                    target.upvotes -= 1; target.downvotes += 1
                else:
                    target.downvotes -= 1; target.upvotes += 1
            existing.value = value
            db.commit()
            return {"message": "Vote updated"}
    else:
        new_vote = Vote(user_id=current_user.id, post_id=post_id, comment_id=comment_id, value=value)
        db.add(new_vote)
        if post_id:
            if value == 1: target.upvotes += 1
            else: target.downvotes += 1
        else:
            if value == 1: target.upvotes += 1
            else: target.downvotes += 1
        db.commit()
        return {"message": "Vote cast"}

if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=PORT)