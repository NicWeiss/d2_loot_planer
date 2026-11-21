"""Currencies the site trades and the fixed game rules around them: groups, cube recipes, crafting ties."""
from dataclasses import dataclass

UNIT = "r24"  # Ist: every value is expressed in Ist-equivalents

# Crafting chains priced as fixed fractions of the top item: code -> (crafts into, how many make one).
CRAFT = {
    "cr1": ("cr3", 144),  # Blacksmith Ingot: 12 -> Horadric Sphere, 12 spheres -> Resplendent Spark
    "cr2": ("cr3", 12),   # Horadric Sphere
}

# Horadric Cube rune upgrades (standard D2R ladder recipes):
# 3 runes -> next up to Pul, 2 runes -> next from Pul on; from Thul on each step also eats a gem.
_CUBE_GEMS = ["gcy", "gcv", "gcb", "gcr", "gcg", "gcw", "gfy", "gfv", "gfb", "gfr", "gfg", "gfw",
              "gsy", "gsv", "gsb", "gsr", "gsg", "gsw", "gly", "gzv", "glb", "glr", "glg"]
RUNE_CUBE: dict[str, dict[str, int]] = {}
for _n in range(1, 33):
    _recipe = {f"r{_n:02d}": 3 if _n < 21 else 2}
    if _n >= 10:
        _recipe[_CUBE_GEMS[_n - 10]] = 1
    RUNE_CUBE[f"r{_n + 1:02d}"] = _recipe
# Every cube recipe; the four Essences make a Token of Absolution.
CUBE: dict[str, dict[str, int]] = {**RUNE_CUBE, "toa": {"tes": 1, "ceh": 1, "bet": 1, "fed": 1}}

# Lem and up plus Deep shards: spendable like money for high runes.
CORE = frozenset({f"r{i}" for i in range(20, 34)} | {"xa4"})

GROUPS = ["runes", "keys", "uber", "essences", "shards", "statues", "craft", "gems", "other"]


@dataclass(frozen=True)
class Currency:
    code: str
    name: str
    type_code: str
    item_class: str

    @property
    def display(self) -> str:
        return self.name.replace(" Rune", "")

    @property
    def group(self) -> str:
        c = self.code
        if self.type_code == "rune":
            return "runes"
        if c.startswith("pk"):
            return "keys"
        if c in ("dhn", "bey", "mbr", "toa", "std", "sth", "jwd"):
            return "uber"
        if c in ("tes", "ceh", "bet", "fed"):
            return "essences"
        if c.startswith("xa"):
            return "shards"
        if c.startswith("ua"):
            return "statues"
        if c.startswith("cr"):
            return "craft"
        if self.item_class == "gem":
            return "gems"
        return "other"


class Catalog:
    """Every stackable currency the site knows, by code."""

    def __init__(self, currencies: list[Currency]):
        self.by_code = {c.code: c for c in currencies}

    @classmethod
    def from_site(cls, raw: dict) -> "Catalog":
        return cls([Currency(c["code"], c["name"], c["typeCode"], c["itemClass"]) for c in raw["currencyItems"]])

    def __contains__(self, code: str) -> bool:
        return code in self.by_code

    def __getitem__(self, code: str) -> Currency:
        return self.by_code[code]

    def name(self, code: str) -> str:
        return self.by_code[code].name if code in self.by_code else code


def item_key(name: str | None, quality: str | None) -> str:
    """Non-currency goods are traded by name and quality; copies differ in rolls."""
    return f"i:{name}|{quality}"


def own(key: str) -> str:
    """Your own copy of an item: only that can fill someone's WTB for it."""
    return "own:" + key
