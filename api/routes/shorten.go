package routes

import (
	"os"
	"strconv"
	"time"

	"github.com/asaskevich/govalidator"
	"github.com/esankhan/url-shortner/database"
	"github.com/esankhan/url-shortner/helpers"
	"github.com/go-redis/redis/v8"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)


type request struct {
	URL     string `json:"url"`
	CustomShort string `json:"short"`
	Expiry	time.Duration `json:"expiry"`
}

type response struct {
	URL    string `json:"url"`
	CustomShort string `json:"short"`
	Expiry time.Duration `json:"expiry"`
	XRateRemaining int `json:"rate_limit"`
	XRateLimitReset  time.Duration `json:"x_rate_limit_reset"` 
}


func ShortenURL(c *fiber.Ctx) error {

	body := new(request)
	if err := c.BodyParser(body); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Cannot parse JSON",
		})
	}

	//implement rate limiting

	redis2 := database.CreateClient(1)
	defer redis2.Close()
	_ , err := redis2.Get(database.Ctx, c.IP()).Result()

	if err == redis.Nil {
		_ = redis2.Set(database.Ctx, c.IP(), os.Getenv(("API_QUOTA")),30*60*time.Second).Err()
	} else {
		value,_ := redis2.Get(database.Ctx, c.IP()).Result()
		valInt, _ := strconv.Atoi(value)
		if valInt <= 0 {
			limit,_ := redis2.TTL(database.Ctx, c.IP()).Result()
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "Rate limit exceeded",
				"rate_limit": limit/time.Nanosecond/time.Minute,
			})
		} 
	}

	// check if the URL is valid

	if !govalidator.IsURL(body.URL) {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid URL",
		})
	}

	// check for domain error

	if !helpers.RemoveDomainError(body.URL){
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"error": "URL is not allowed",
		})
	}

	//enforce https

	body.URL = helpers.EnforceHTTPS(body.URL)
	var id string
	if body.CustomShort != "" {
		id = body.CustomShort
	} else {
		id = uuid.New().String()[:6]
	}
	redis3 := database.CreateClient(0)
	defer redis3.Close()

val, _ := redis3.Get(database.Ctx, id).Result()	

if val != "" {
	return c.Status(fiber.StatusConflict).JSON(fiber.Map{
		"error": "Short URL already exists",
	})
}

if body.Expiry == 0 {
	body.Expiry = 24 
}

err = redis3.Set(database.Ctx,id,body.URL,body.Expiry*3600*time.Second).Err()

if err != nil {
	return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
		"error": "Cannot connect to DB",
	})
}

resp := response{
	URL: body.URL,
	CustomShort: "",
	Expiry: body.Expiry,
	XRateRemaining: 10,
	XRateLimitReset: 30,
}

	redis2.Decr(database.Ctx, c.IP())
	//return nil

	val,_ = redis2.Get(database.Ctx, c.IP()).Result()
	resp.XRateRemaining, _ = strconv.Atoi(val)

	ttl, _ := redis2.TTL(database.Ctx, c.IP()).Result()
	resp.XRateLimitReset = ttl/time.Nanosecond/time.Minute

	resp.CustomShort = os.Getenv("DOMAIN") + "/" + id
	return c.Status(fiber.StatusOK).JSON(resp)
}