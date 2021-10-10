package main

// TODO
// study, like at least a bit pls
// refactor code
// epic effects (particles) when a large portion of velocity was preserved when entering hook
// investigate weird vibration if you just hang on the hook for a while
// fix fucking NaN crashes when chilling on hook for too long
// lock hook to center of tile
// movement:
//	-hook settings: snapping to nearest 30/45 deg? setting hook points with click and using with space or using in current pos with just mouse?
// split functionality of move into move and checkCollision
// is Move() return-instantly-on-collision bool flag parameter really necessary? couldn't it just basically be always true and not make a difference because of the special Move() loop structure?

import (
	"fmt"
	"image/color"
	"log"
	"math"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

func Round(n float64) int {
	return int(math.Round(n))
}

const (
	SCALE_FACTOR = 4

	MAX_PLAYER_VX                  = 2.5
	MAX_VX_EXCEEDED_SLOWDOWN_COEFF = 0.95
	PLAYER_X_ACCELL                = 0.4
	AIR_X_ACCELL_COEFF             = 0.4 // higher = more air control
	FRICTION_COEFFICIENT_GROUND    = 0.55
	FRICION_COEFFICIENT_AIR        = 0.1

	GRAV_ACCELL               = 0.35
	JUMP_INSTANT_ACCELL       = 4.0
	TOTAL_EXTRA_JUMP_DELTA_VY = 3.0
	JUMP_ACCELL_FRAME_COUNT   = 10
	JUMP_ACCELL_FRAME_START   = 4

	MAX_HOOK_RADIUS     = 80.0
	HOOK_SPEEDUP_COEFF  = 1.0
	HOOK_FRICTION_COEFF = 0.01

	BUFFER_FRAME_COUNT = 5
)

// currently: pixel grid is 480x270, tile grid is 80x45 (6 logical pixels per tile)
type TileType uint32

const (
	EMPTY           = TileType(0) // tileMap[i].tileType == 0 means no tile there
	GROUND          = TileType(SOLID)
	SPIKE           = TileType(SOLID | KILLS_ON_CONTACT)
	TILE_WIDTH      = 6
	TILE_MAP_WIDTH  = 80 // tiles
	TILE_MAP_HEIGHT = 45 // tiles
	TILE_COUNT      = TILE_MAP_WIDTH * TILE_MAP_HEIGHT
)

const ( // possible flags, combined to create new tile types
	SOLID              uint32 = 1 << iota // = 1
	KILLS_ON_CONTACT                      // = 2
	SOMETHING_ELSE_IDK                    // = 4, etc
	TEST               = SOLID & KILLS_ON_CONTACT
)

type TileMap struct {
	image    [TILE_COUNT]*ebiten.Image
	tileType [TILE_COUNT]TileType
}

func (g *Game) getTileImage(t TileType) *ebiten.Image {
	switch t {
	case EMPTY:
		return nil
	case GROUND:
		return g.tileImage
	case SPIKE:
		return g.tileImageSpike
	default:
		return nil
	}
}

// TODO: prolly change this
func (t *TileMap) set(x, y int, image *ebiten.Image, tileType TileType) {
	ind := y*TILE_MAP_WIDTH + x
	t.image[ind] = image
	t.tileType[ind] = tileType
}

func (t *TileMap) Draw(screen *ebiten.Image) {
	options := &ebiten.DrawImageOptions{}
	for i := 0; i < TILE_MAP_HEIGHT; i++ {
		for j := 0; j < TILE_MAP_WIDTH; j++ {
			if t.tileType[i*TILE_MAP_WIDTH+j] != EMPTY {
				screen.DrawImage(t.image[i*TILE_MAP_WIDTH+j], options)
			}
			options.GeoM.Translate(float64(TILE_WIDTH), 0.0)
		}
		options.GeoM.Translate(-float64(TILE_MAP_WIDTH*TILE_WIDTH), float64(TILE_WIDTH))
	}
}

type Circle struct {
	x, y              float64
	r                 float64
	clr               color.Color
	pixels            *ebiten.Image
	translationMatrix *ebiten.DrawImageOptions
}

func (c *Circle) Update() {
	c.pixels.Clear()

	ir := Round(c.r)
	ix := Round(MAX_HOOK_RADIUS)
	iy := Round(MAX_HOOK_RADIUS)
	x, y, dx, dy := ir-1, 0, 1, 1
	err := dx - (ir * 2)

	for x > y {
		c.pixels.Set(ix+x, iy+y, c.clr)
		c.pixels.Set(ix+y, iy+x, c.clr)
		c.pixels.Set(ix-y, iy+x, c.clr)
		c.pixels.Set(ix-x, iy+y, c.clr)
		c.pixels.Set(ix-x, iy-y, c.clr)
		c.pixels.Set(ix-y, iy-x, c.clr)
		c.pixels.Set(ix+y, iy-x, c.clr)
		c.pixels.Set(ix+x, iy-y, c.clr)

		if err <= 0 {
			y++
			err += dy
			dy += 2
		}
		if err > 0 {
			x--
			dx += 2
			err += dx - (ir * 2)
		}
	}
	c.translationMatrix = &ebiten.DrawImageOptions{}
	c.translationMatrix.GeoM.Translate(c.x-MAX_HOOK_RADIUS, c.y-MAX_HOOK_RADIUS)
}

type Player struct {
	vx, vy float64 // velocity
	e      Entity  // pos, remainder, height, width all here

	movingLeft          bool
	movingRight         bool
	falling             bool
	grounded            bool
	framesSinceGrounded int

	inHook       bool
	enteringHook bool
	hookX        float64
	hookY        float64
	hookR        float64

	framesSinceHookEnterAttempt int
	framesSinceJumpAttempt      int

	imageOptions *ebiten.DrawImageOptions
	sprite       *ebiten.Image
	spriteInHook *ebiten.Image
}

type Entity struct {
	x, y          int
	remX, remY    float64
	height, width int
}

func (player *Player) Draw(screen *ebiten.Image) {
	player.imageOptions.GeoM = ebiten.GeoM{}
	player.imageOptions.GeoM.Translate(float64(player.e.x), float64(player.e.y))
	if player.inHook {
		screen.DrawImage(player.spriteInHook, player.imageOptions)
	} else {
		screen.DrawImage(player.sprite, player.imageOptions)
	}
}

// returns true iff moving the player by dx dy would move the player into level geometry
// dx and dy must be in {-1,0, 1} and only one of them should be non-zero at a time

// directions
type Direction uint8

const (
	// TODO: remove all but one iota
	UP Direction = 1 << iota
	RIGHT
	DOWN
	LEFT
	UPLEFT        = UP | LEFT
	UPRIGHT       = UP | RIGHT
	DOWNLEFT      = DOWN | LEFT
	DOWNRIGHT     = DOWN | RIGHT
	NIL_DIRECTION = Direction(0)
)

// should this also return which tile is being collided with???
func (e *Entity) collidesAt(direction Direction, tileMap *TileMap) bool {
	// inefficient but makes collision work no matter the
	// relationship between tile and player hitbox sizes
	switch direction {
	case UP, DOWN:
		nextY := e.y + e.height
		if direction == UP {
			nextY = e.y - 1
		}
		tileY := nextY / TILE_WIDTH
		// check for out of bounds
		if tileY >= TILE_MAP_HEIGHT || tileY < 0 {
			return false // or true???? or panic??
		}
		for i := e.x; i < e.x+e.width; i++ {
			tileX := i / TILE_WIDTH
			if uint32(tileMap.tileType[TILE_MAP_WIDTH*tileY+tileX])&SOLID == SOLID { // colliding
				return true
			}
		}
	case LEFT, RIGHT:
		nextX := e.x + e.width
		if direction == LEFT {
			nextX = e.x - 1
		}
		tileX := nextX / TILE_WIDTH
		// check for out of bounds
		if tileX >= TILE_MAP_WIDTH || tileX < 0 {
			return false // or true???? or panic??
		}
		for i := e.y; i < e.y+e.height; i++ {
			tileY := i / TILE_WIDTH
			if uint32(tileMap.tileType[TILE_MAP_WIDTH*tileY+tileX])&SOLID == SOLID { // colliding
				return true
			}
		}
	default:
		panic(fmt.Sprintf("CheckIfCollides() got passed invalid direction %v", direction))
	}
	return false
}

// returns the collision direction
// (if there's a corner and two collisions happen in one Move() then it returns a compound direction)
// (returns NIL_DIRECTION with no collision)
func (e *Entity) Move(x, y float64, tileMap *TileMap, stopCompletelyOnCollision bool) Direction {
	// calculate dx and dy, the integer distances to move the player by
	// indX = -1
	// indY = -1
	e.remX += x
	dx := int(e.remX)
	e.remX = math.Mod(e.remX, 1)

	e.remY += y
	dy := int(e.remY)
	e.remY = math.Mod(e.remY, 1)

	// perform the actual movement
	stepsX, stepsY := 0, 0
	signX, signY, absDX, absDY := 1, 1, dx, dy
	if dx < 0 {
		signX = -1
		absDX = -dx
	}
	if dy < 0 {
		signY = -1
		absDY = -dy
	}
	// the idea of the loop is to alternate the direction (X or Y) in which we're moving
	// in a way which approximates the 'real' trajectory, so with dx=200 dy=100, we'd move
	// 2 right, 1 down, 2 right, 1 down etc.
	retDir := NIL_DIRECTION
	for stepsX < absDX || stepsY < absDY {
		if absDX == 0 || stepsX*absDY > absDX*stepsY && stepsY < absDY {
			// check for collisions
			dir := DOWN
			if signY == -1 {
				dir = UP
			}
			if e.collidesAt(dir, tileMap) {
				if stopCompletelyOnCollision {
					return dir
				}
				retDir |= dir
				stepsY = absDY
				e.y -= signY // cancel out the last incremenet that follows the break
			}
			e.y += signY
			stepsY++
		} else {
			dir := RIGHT
			if signX == -1 {
				dir = LEFT
			}
			if e.collidesAt(dir, tileMap) {
				if stopCompletelyOnCollision {
					return dir
				}
				retDir |= dir
				stepsX = absDX
				e.x -= signX
			}
			e.x += signX
			stepsX++
		}

	}
	return retDir
}

type Game struct {
	width  int
	height int

	player          *Player
	circle          *Circle
	indicatorCircle *Circle

	a1, b1, a2, b2 float64

	tileMap        *TileMap
	tileImage      *ebiten.Image // TEMPORARY
	tileImageSpike *ebiten.Image // TEMPORARY
}

func (g *Game) drawLineEquation(a, b float64, screen *ebiten.Image) {
	if a == 0 {
		return
	}
	x1 := -b / a
	y1 := 0.0

	x2 := (float64(g.height) - b) / a
	y2 := float64(g.height)

	clr := color.RGBA{80, 80, 80, 255}
	ebitenutil.DrawLine(screen, x1, y1, x2, y2, clr)

}

func (g *Game) Update() error {
	{ // update player
		playerCenterX := float64(g.player.e.x) + float64(g.player.e.width)/2.0
		playerCenterY := float64(g.player.e.y) + float64(g.player.e.height)/2.0
		if ebiten.IsKeyPressed(ebiten.KeyA) {
			g.player.movingLeft = true
		}
		if ebiten.IsKeyPressed(ebiten.KeyD) {
			g.player.movingRight = true
		}
		// accelerate in x direction if not at max vx
		if g.player.movingLeft && !g.player.movingRight && !g.player.inHook && g.player.vx > -MAX_PLAYER_VX {
			d := PLAYER_X_ACCELL
			if !g.player.grounded {
				d *= AIR_X_ACCELL_COEFF
			}
			g.player.vx = math.Max(-MAX_PLAYER_VX, g.player.vx-d)
		}
		if g.player.movingRight && !g.player.movingLeft && !g.player.inHook && g.player.vx < MAX_PLAYER_VX {
			d := PLAYER_X_ACCELL
			if !g.player.grounded {
				d *= AIR_X_ACCELL_COEFF
			}
			g.player.vx = math.Min(MAX_PLAYER_VX, g.player.vx+d)
		}
		if inpututil.IsKeyJustPressed(ebiten.KeyW) {
			g.player.framesSinceJumpAttempt = 0
		} else {
			g.player.framesSinceJumpAttempt++
		}
		if g.player.grounded && g.player.framesSinceJumpAttempt < BUFFER_FRAME_COUNT {
			g.player.grounded = false
			g.player.vy -= JUMP_INSTANT_ACCELL
		}
		if !ebiten.IsKeyPressed(ebiten.KeyW) {
			g.player.framesSinceGrounded = JUMP_ACCELL_FRAME_COUNT + JUMP_ACCELL_FRAME_START + 1
		}

		// gravity
		if !g.player.grounded {
			g.player.vy += GRAV_ACCELL
		}
		if g.player.framesSinceGrounded < (JUMP_ACCELL_FRAME_COUNT+JUMP_ACCELL_FRAME_START) && g.player.framesSinceGrounded >= JUMP_ACCELL_FRAME_START {
			g.player.vy -= (TOTAL_EXTRA_JUMP_DELTA_VY / JUMP_ACCELL_FRAME_COUNT)
			// fmt.Printf("applying extra dvx=%v on frame %v; at height %v\n", (totalExtraJumpDeltaVX / jumpAcellFrameCount), g.player.framesSinceGrounded, g.player.y)
		}

		{ // enter hook
			if inpututil.IsKeyJustPressed(ebiten.KeySpace) {
				g.player.framesSinceHookEnterAttempt = 0
			} else {
				g.player.framesSinceHookEnterAttempt++
			}
			lx := playerCenterX - g.circle.x
			ly := playerCenterY - g.circle.y
			if !g.player.inHook && g.player.framesSinceHookEnterAttempt < BUFFER_FRAME_COUNT &&
				math.Sqrt(lx*lx+ly*ly) <= MAX_HOOK_RADIUS {
				g.player.enteringHook = true
				g.player.framesSinceHookEnterAttempt = BUFFER_FRAME_COUNT + 1
				g.player.vx *= HOOK_SPEEDUP_COEFF
				g.player.vy *= HOOK_SPEEDUP_COEFF
				if g.player.vx > 0 {
					g.player.vx += math.Sqrt(g.player.vx+1) * HOOK_SPEEDUP_COEFF
				} else {
					g.player.vx -= math.Sqrt(math.Abs(g.player.vx)+1) * HOOK_SPEEDUP_COEFF
				}
				if g.player.vy > 0 {
					g.player.vy += math.Sqrt(g.player.vy+1) * HOOK_SPEEDUP_COEFF
				} else {
					g.player.vy -= math.Sqrt(math.Abs(g.player.vy)+1) * HOOK_SPEEDUP_COEFF
				}

				g.player.inHook = true
				g.circle.clr = color.RGBA{255, 255, 255, 255}
			}
		}
		if g.player.inHook && inpututil.IsKeyJustReleased(ebiten.KeySpace) {
			g.player.inHook = false
			// potential exit velocity multiplier here?
		}

		// friction (if not moving left or right)
		// moving left at right at once == not moving
		if (g.player.movingLeft == g.player.movingRight) && !g.player.inHook {
			c := 0.0
			if g.player.grounded {
				c = 1 - FRICTION_COEFFICIENT_GROUND
			} else {
				c = 1 - FRICION_COEFFICIENT_AIR
			}
			g.player.vx *= c
		}

		// make velocity perpendicular to hook circle radius and apply hook friciton
		if g.player.inHook && g.a1 != g.a2 {
			x, y := playerCenterX+g.player.vx, playerCenterY+g.player.vy
			g.player.vx = (g.b2 - y + g.a1*x) / (g.a1 - g.a2)
			g.player.vy = g.a2*g.player.vx + g.b2
			g.player.vx = g.player.vx - playerCenterX
			g.player.vy = g.player.vy - playerCenterY
			g.player.vx *= (1 - HOOK_FRICTION_COEFF)
			g.player.vy *= (1 - HOOK_FRICTION_COEFF)
		}

		// max vx check
		if math.Abs(g.player.vx) > MAX_PLAYER_VX && !g.player.inHook {
			excess := math.Abs(g.player.vx) - MAX_PLAYER_VX
			if g.player.vx > 0 {
				g.player.vx = MAX_PLAYER_VX + MAX_VX_EXCEEDED_SLOWDOWN_COEFF*excess
			} else {
				g.player.vx = -(MAX_PLAYER_VX + MAX_VX_EXCEEDED_SLOWDOWN_COEFF*excess)
			}
		}

		{ // move the player, checking for collisions
			dir := g.player.e.Move(g.player.vx, g.player.vy, g.tileMap, false)
			if dir&LEFT == LEFT || dir&RIGHT == RIGHT { // handle X direction collision
				g.player.inHook = false
				g.player.vx = 0
				g.player.e.remX = 0.0
			}
			if dir&UP == UP || dir&DOWN == DOWN { // handle Y direction collision
				g.player.inHook = false
				g.player.vy = 0
				g.player.e.remY = 0
				g.player.framesSinceGrounded = JUMP_ACCELL_FRAME_COUNT + JUMP_ACCELL_FRAME_START + 1
			}
		}

		// recalculate (first checking for division by 0)
		playerCenterX = float64(g.player.e.x) + float64(g.player.e.width)/2.0
		playerCenterY = float64(g.player.e.y) + float64(g.player.e.height)/2.0
		if playerCenterY-g.circle.y != 0 && playerCenterX-g.circle.x != 0 {
			// playerCenterX, playerCenterY = float64(g.player.x), float64(g.player.y)
			g.a1 = (playerCenterY - g.circle.y) / (playerCenterX - g.circle.x)
			g.b1 = playerCenterY - playerCenterX*g.a1
			g.a2 = -1 / g.a1
			g.b2 = playerCenterY - g.a2*playerCenterX
		}
		if g.a2 != 0 && g.player.inHook && playerCenterX != g.circle.x { // counteract circular motion inaccuracy (snap back to circle)
			// maths
			a := (g.a1*g.a1 + 1)
			b := (2*g.a1*g.b1 - 2*g.a1*g.circle.y - 2*g.circle.x)
			c := (g.circle.x * g.circle.x) + (g.circle.y * g.circle.y) + (g.b1 * g.b1) - (g.circle.r * g.circle.r) - (2 * g.b1 * g.circle.y)
			d := b*b - 4*a*c
			x1 := (-b - math.Sqrt(d)) / (2 * a)
			x2 := (-b + math.Sqrt(d)) / (2 * a)
			targetX := 0.0
			if math.Abs(playerCenterX-x1) < math.Abs(playerCenterX-x2) {
				targetX = x1
			} else {
				targetX = x2
			}
			targetY := g.a1*targetX + g.b1
			g.player.e.Move(targetX-playerCenterX, targetY-playerCenterY, g.tileMap, false)
		}

		// recalculate (first checking for division by 0)
		playerCenterX = float64(g.player.e.x) + float64(g.player.e.width)/2.0
		playerCenterY = float64(g.player.e.y) + float64(g.player.e.height)/2.0
		if playerCenterY-g.circle.y != 0 && playerCenterX-g.circle.x != 0 {
			g.a1 = (playerCenterY - g.circle.y) / (playerCenterX - g.circle.x)
			g.b1 = playerCenterY - playerCenterX*g.a1
			g.a2 = -1 / g.a1
			g.b2 = playerCenterY - g.a2*playerCenterX
		}

		{ // check if player is grounded
			if g.player.e.collidesAt(DOWN, g.tileMap) {
				g.player.grounded = true
				g.player.vy = 0.0
				g.player.e.remY = 0.0
				g.player.framesSinceGrounded = 0
			} else {
				g.player.grounded = false
				g.player.framesSinceGrounded++
			}
		}

		g.player.movingLeft = false
		g.player.movingRight = false
		g.player.enteringHook = false
	}

	if !g.player.inHook || g.player.enteringHook { // update circle
		playerCenterX := float64(g.player.e.x) + float64(g.player.e.width)/2.0
		playerCenterY := float64(g.player.e.y) + float64(g.player.e.height)/2.0
		if inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) {
			x, y := ebiten.CursorPosition()
			x1, y1 := float64(x), float64(y)
			dx := (x1 - playerCenterX)
			dy := (y1 - playerCenterY)
			collisionFinder := &Entity{x: int(math.Round(playerCenterX)), y: int(math.Round(playerCenterY)),
				remX: 0.0, remY: 0.0, width: 1, height: 1}
			dir := collisionFinder.Move(1000*dx, 1000*dy, g.tileMap, true)

			if dir == UP {
				g.circle.x = float64(collisionFinder.x)
				// -1 to make it inside the ceiling and not just right under
				g.circle.y = float64(collisionFinder.y - 1)
				g.indicatorCircle.x = g.circle.x
				g.indicatorCircle.y = g.circle.y
				g.indicatorCircle.Update()
			}

		}

		lx := g.circle.x - playerCenterX
		ly := g.circle.y - playerCenterY
		g.circle.r = math.Sqrt(lx*lx + ly*ly)
		c := math.Min(math.Max((1.0/15.0)*(MAX_HOOK_RADIUS-g.circle.r), 0.0), 0.7)
		if g.circle.r > MAX_HOOK_RADIUS {
			g.circle.r = MAX_HOOK_RADIUS
		}
		v := uint8(255 * c)
		g.circle.clr = color.RGBA{v, v, v, 255}
		if g.player.enteringHook {
			g.circle.clr = color.RGBA{255, 255, 255, 255}
		}
		g.circle.Update()

	}

	{ // "level editor"
		placingBlock := false
		selectedTileType := EMPTY
		switch {
		case ebiten.IsKeyPressed(ebiten.Key1):
			selectedTileType = EMPTY
			placingBlock = true
		case ebiten.IsKeyPressed(ebiten.Key2):
			selectedTileType = GROUND
			placingBlock = true
		case ebiten.IsKeyPressed(ebiten.Key3):
			selectedTileType = SPIKE
			placingBlock = true
		}
		if placingBlock {
			x, y := ebiten.CursorPosition()
			tileX, tileY := x/6, y/6
			ind := tileY*TILE_MAP_WIDTH + tileX
			g.tileMap.image[ind] = g.getTileImage(selectedTileType)
			g.tileMap.tileType[ind] = selectedTileType
		}
	}
	return nil
}

func (g *Game) Draw(screen *ebiten.Image) {
	screen.DrawImage(g.circle.pixels, g.circle.translationMatrix)
	screen.DrawImage(g.indicatorCircle.pixels, g.indicatorCircle.translationMatrix)

	g.tileMap.Draw(screen)
	g.player.Draw(screen)

	ebitenutil.DebugPrint(screen, fmt.Sprintf("%v\n%04v %04v\n %.4f %.4f\n%v", ebiten.CurrentFPS(), g.player.e.x, g.player.e.y, g.player.vx, g.player.vy, g.player.grounded))
	// fmt.Printf("   x: %04v,    y: %04v\n", g.player.e.x, g.player.e.y)
	// fmt.Printf("remX: %.2f, remY: %.2f\n\n", g.player.e.remX, g.player.e.remY)

	// g.drawLineEquation(g.a1, g.b1, screen)
	// g.drawLineEquation(g.a2, g.b2, screen)
}

func (g *Game) Layout(outsideWidth, outsideHeight int) (int, int) {
	return g.width, g.height
}

func (g *Game) Init() {
	x, y := ebiten.ScreenSizeInFullscreen()
	g.width = x / SCALE_FACTOR
	g.height = y / SCALE_FACTOR

	{ // initialize player
		g.player = &Player{e: Entity{x: 100, y: 100, height: 10, width: 8}, vx: 0, vy: 0, movingLeft: false, movingRight: false, falling: false, grounded: false, framesSinceGrounded: 0,
			inHook: false, enteringHook: false, hookX: 0.0, hookY: 0.0, hookR: 0.0, imageOptions: &ebiten.DrawImageOptions{}, framesSinceHookEnterAttempt: 0, framesSinceJumpAttempt: 0}
		g.player.sprite = ebiten.NewImage(g.player.e.width, g.player.e.height)
		g.player.sprite.Fill(color.RGBA{255, 255, 255, 255})
		g.player.spriteInHook = ebiten.NewImage(g.player.e.width, g.player.e.height)
		g.player.spriteInHook.Fill(color.RGBA{180, 0, 0, 255})
	}

	g.circle = &Circle{x: 200.0, y: 200.0, r: 60.0, clr: color.RGBA{120, 120, 120, 255},
		pixels: ebiten.NewImage(Round(MAX_HOOK_RADIUS*2.0), Round(MAX_HOOK_RADIUS*2.0)), translationMatrix: &ebiten.DrawImageOptions{}}
	g.indicatorCircle = &Circle{x: 0.0, y: 0.0, r: 8.0, clr: color.RGBA{180, 0, 0, 255},
		pixels: ebiten.NewImage(Round(MAX_HOOK_RADIUS*2.0), Round(MAX_HOOK_RADIUS*2.0)), translationMatrix: &ebiten.DrawImageOptions{}}

	{ // "level design" lol
		g.tileMap = &TileMap{}
		g.tileImage = ebiten.NewImage(TILE_WIDTH, TILE_WIDTH)
		g.tileImage.Fill(color.RGBA{120, 120, 120, 255})
		g.tileImageSpike = ebiten.NewImage(TILE_WIDTH, TILE_WIDTH)
		g.tileImageSpike.Fill(color.RGBA{255, 50, 50, 255})
		for i := 0; i < TILE_MAP_WIDTH; i++ {
			g.tileMap.set(i, TILE_MAP_HEIGHT-1, g.tileImage, GROUND)
			g.tileMap.set(i, 0, g.tileImage, GROUND)
		}
		for i := 0; i < TILE_MAP_HEIGHT; i++ {
			g.tileMap.set(0, i, g.tileImage, GROUND)
			g.tileMap.set(TILE_MAP_WIDTH-1, i, g.tileImage, GROUND)
		}
		for i := 0; i < TILE_MAP_WIDTH/2; i++ {
			g.tileMap.set(i, 30, g.tileImage, GROUND)
		}
		g.tileMap.set(0, 0, g.tileImage, GROUND)
		g.tileMap.set(3, 0, g.tileImage, GROUND)
		g.tileMap.set(3, 33, g.tileImage, GROUND)
		g.tileMap.set(10, 30, g.tileImage, GROUND)
		g.tileMap.set(11, 30, g.tileImage, GROUND)
		g.tileMap.set(12, 30, g.tileImage, GROUND)
		g.tileMap.set(13, 30, g.tileImage, GROUND)
	}
}

func main() {
	ebiten.SetWindowSize(ebiten.ScreenSizeInFullscreen())
	ebiten.SetWindowTitle("Polygons (Ebiten Demo)")
	// ebiten.SetMaxTPS(ebiten.UncappedTPS)
	// ebiten.SetVsyncEnabled(false)
	g := &Game{}
	g.Init()
	if err := ebiten.RunGame(g); err != nil {
		log.Fatal(err)
	}
}
