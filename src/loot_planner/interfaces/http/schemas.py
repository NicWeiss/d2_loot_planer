"""Request bodies."""
from typing import Literal

from pydantic import BaseModel, Field


class LoginBody(BaseModel):
    login: str = Field(min_length=1, max_length=100)
    password: str = Field(min_length=1, max_length=200)
    remember: bool = False


class UploadBody(BaseModel):
    name: str = Field(default="файл", max_length=100)
    data: dict


class RouteBody(BaseModel):
    target: str = Field(min_length=1, max_length=300)
    qty: int = Field(default=1, ge=1, le=20)
    stashMode: Literal["all", "currency", "gems", "none"] = "all"
    keys: list[Literal["pk1", "pk2", "pk3"]] = []
    useCube: bool = True
    effort: float = Field(default=0.03, ge=0, le=1)
