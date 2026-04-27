--
-- PostgreSQL database dump
--


-- Dumped from database version 18.3
-- Dumped by pg_dump version 18.3

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET transaction_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: freelance_dispatcher_cargo_favorites; Type: TABLE; Schema: public; Owner: -
--

CREATE TABLE public.freelance_dispatcher_cargo_favorites (
    id uuid DEFAULT public.uuid_generate_v4() NOT NULL,
    dispatcher_id uuid NOT NULL,
    cargo_id uuid NOT NULL,
    created_at timestamp without time zone DEFAULT now() NOT NULL
);


--
-- Name: freelance_dispatcher_cargo_favorites freelance_dispatcher_cargo_favorites_dispatcher_id_cargo_id_key; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatcher_cargo_favorites
    ADD CONSTRAINT freelance_dispatcher_cargo_favorites_dispatcher_id_cargo_id_key UNIQUE (dispatcher_id, cargo_id);


--
-- Name: freelance_dispatcher_cargo_favorites freelance_dispatcher_cargo_favorites_pkey; Type: CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatcher_cargo_favorites
    ADD CONSTRAINT freelance_dispatcher_cargo_favorites_pkey PRIMARY KEY (id);


--
-- Name: idx_freelance_dispatcher_cargo_favorites_cargo; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_freelance_dispatcher_cargo_favorites_cargo ON public.freelance_dispatcher_cargo_favorites USING btree (cargo_id);


--
-- Name: idx_freelance_dispatcher_cargo_favorites_dispatcher; Type: INDEX; Schema: public; Owner: -
--

CREATE INDEX idx_freelance_dispatcher_cargo_favorites_dispatcher ON public.freelance_dispatcher_cargo_favorites USING btree (dispatcher_id);


--
-- Name: freelance_dispatcher_cargo_favorites freelance_dispatcher_cargo_favorites_cargo_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatcher_cargo_favorites
    ADD CONSTRAINT freelance_dispatcher_cargo_favorites_cargo_id_fkey FOREIGN KEY (cargo_id) REFERENCES public.cargo(id) ON DELETE CASCADE;


--
-- Name: freelance_dispatcher_cargo_favorites freelance_dispatcher_cargo_favorites_dispatcher_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: -
--

ALTER TABLE ONLY public.freelance_dispatcher_cargo_favorites
    ADD CONSTRAINT freelance_dispatcher_cargo_favorites_dispatcher_id_fkey FOREIGN KEY (dispatcher_id) REFERENCES public.freelance_dispatchers(id) ON DELETE CASCADE;


--
-- PostgreSQL database dump complete
--


